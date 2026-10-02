package elfdeps

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// writeELF writes a minimal 64-bit ELF with a dynamic section: the needed
// libraries and an optional RUNPATH. Enough for debug/elf, never runnable.
func writeELF(t *testing.T, path string, machine elf.Machine, needed []string, runpath string) {
	t.Helper()
	dynstr := []byte{0}
	offset := func(s string) uint64 {
		o := uint64(len(dynstr))
		dynstr = append(append(dynstr, s...), 0)
		return o
	}
	var dyn bytes.Buffer
	entry := func(tag elf.DynTag, val uint64) {
		binary.Write(&dyn, binary.LittleEndian, int64(tag))
		binary.Write(&dyn, binary.LittleEndian, val)
	}
	for _, n := range needed {
		entry(elf.DT_NEEDED, offset(n))
	}
	if runpath != "" {
		entry(elf.DT_RUNPATH, offset(runpath))
	}
	entry(elf.DT_NULL, 0)
	shstr := []byte("\x00.dynstr\x00.dynamic\x00.shstrtab\x00")

	const ehsize, shentsize = 64, 64
	dynstrOff := uint64(ehsize)
	dynOff := dynstrOff + uint64(len(dynstr))
	shstrOff := dynOff + uint64(dyn.Len())
	shOff := shstrOff + uint64(len(shstr))

	var b bytes.Buffer
	b.Write([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	w(uint16(elf.ET_DYN))
	w(uint16(machine))
	w(uint32(1))
	w(uint64(0))         // entry
	w(uint64(0))         // phoff
	w(shOff)             // shoff
	w(uint32(0))         // flags
	w(uint16(ehsize))    // ehsize
	w(uint16(56))        // phentsize
	w(uint16(0))         // phnum
	w(uint16(shentsize)) // shentsize
	w(uint16(4))         // shnum
	w(uint16(3))         // shstrndx
	b.Write(dynstr)
	b.Write(dyn.Bytes())
	b.Write(shstr)
	section := func(name uint32, typ elf.SectionType, off, size uint64, link uint32, entsize uint64) {
		w(name)
		w(uint32(typ))
		w(uint64(0)) // flags
		w(uint64(0)) // addr
		w(off)
		w(size)
		w(link)
		w(uint32(0)) // info
		w(uint64(1)) // addralign
		w(entsize)
	}
	section(0, elf.SHT_NULL, 0, 0, 0, 0)
	section(1, elf.SHT_STRTAB, dynstrOff, uint64(len(dynstr)), 0, 0)
	section(9, elf.SHT_DYNAMIC, dynOff, uint64(dyn.Len()), 1, 16)
	section(18, elf.SHT_STRTAB, shstrOff, uint64(len(shstr)), 0, 0)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}
}

func hostMachine(t *testing.T) elf.Machine {
	m, ok := machines[runtime.GOARCH]
	if !ok {
		t.Skip("no ELF machine for", runtime.GOARCH)
	}
	return m
}

func touch(t *testing.T, path string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFindsMissingLibraries(t *testing.T) {
	m := hostMachine(t)
	tmp := t.TempDir()
	sys := filepath.Join(tmp, "usr", "lib")
	root := filepath.Join(tmp, "app", "v1")
	touch(t, filepath.Join(sys, "libc.so.6"))

	// The executable needs libc (system), libbundled (in the app's lib/,
	// found by name), libown (next to it via $ORIGIN/../lib) and libwebkit
	// (nowhere). libbundled in turn needs libgone (nowhere).
	exe := filepath.Join(root, "bin", "app")
	writeELF(t, exe, m, []string{"libc.so.6", "libbundled.so.1", "libown.so", "libwebkit2gtk-4.1.so.0"}, "$ORIGIN/../lib")
	writeELF(t, filepath.Join(root, "lib", "libown.so"), m, []string{"libc.so.6"}, "")
	writeELF(t, filepath.Join(root, "share", "deep", "libbundled.so.1"), m, []string{"libgone.so.2", "libc.so.6"}, "")

	res, err := Check(exe, root, []string{sys})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"libgone.so.2", "libwebkit2gtk-4.1.so.0"}; res.WrongArch || !reflect.DeepEqual(res.Missing, want) {
		t.Errorf("result = %+v, want missing %v", res, want)
	}
}

// A RUNPATH outside the app (e.g. /opt/foo/lib) that has the library counts
// as found, but is not followed.
func TestCheckRunpathOutsideApp(t *testing.T) {
	m := hostMachine(t)
	tmp := t.TempDir()
	opt := filepath.Join(tmp, "opt", "lib")
	touch(t, filepath.Join(opt, "libfoo.so"))
	exe := filepath.Join(tmp, "app", "app")
	writeELF(t, exe, m, []string{"libfoo.so"}, opt)
	res, err := Check(exe, filepath.Join(tmp, "app"), nil)
	if err != nil || len(res.Missing) != 0 {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestCheckWrongArchAndScripts(t *testing.T) {
	m := hostMachine(t)
	other := elf.EM_AARCH64
	if m == elf.EM_AARCH64 {
		other = elf.EM_X86_64
	}
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "app")
	writeELF(t, exe, other, []string{"libnothere.so"}, "")
	res, err := Check(exe, tmp, nil)
	if err != nil || !res.WrongArch || res.Machine == "" || len(res.Missing) != 0 {
		t.Errorf("wrong arch: %+v %v", res, err)
	}

	script := filepath.Join(tmp, "run.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nexec python3 -m app\n"), 0o755)
	if _, err := Check(script, tmp, nil); !errors.Is(err, ErrNotELF) {
		t.Errorf("script: %v", err)
	}
}

func TestSystemDirsFollowIncludes(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "conf.d"), 0o755)
	os.WriteFile(filepath.Join(tmp, "ld.so.conf"), []byte("# comment\ninclude conf.d/*.conf\n/opt/a/lib # trailing\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "conf.d", "b.conf"), []byte("/opt/b/lib\n"), 0o644)
	dirs := systemDirs(filepath.Join(tmp, "ld.so.conf"))
	want := []string{"/opt/b/lib", "/opt/a/lib", "/usr/lib"}
	if !reflect.DeepEqual(dirs[:3], want) {
		t.Errorf("dirs = %v", dirs)
	}
}

// A real system binary resolves everything through the real linker
// configuration: no false alarms on this machine.
func TestCheckRealBinary(t *testing.T) {
	exe := "/usr/bin/ls"
	if f, err := elf.Open(exe); err != nil {
		t.Skip("no ELF ls:", err)
	} else {
		f.Close()
	}
	res, err := Check(exe, t.TempDir(), SystemDirs())
	if err != nil || res.WrongArch || len(res.Missing) != 0 {
		t.Errorf("ls: %+v %v", res, err)
	}
}
