package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
)

var elfBin = []byte("\x7fELF\x02\x01\x01\x00fake-binary")

type entry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func tarBytes(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Linkname: e.link, Size: int64(len(e.body))}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	return buf.Bytes()
}

func tarGz(t *testing.T, entries []entry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(tarBytes(t, entries))
	gz.Close()
	return buf.Bytes()
}

func tarZst(t *testing.T, entries []entry) []byte {
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	zw.Write(tarBytes(t, entries))
	zw.Close()
	return buf.Bytes()
}

func zipBytes(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := fs.FileMode(e.mode)
		if mode == 0 {
			mode = 0o644
		}
		if e.link != "" {
			mode |= fs.ModeSymlink
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if e.link != "" {
			w.Write([]byte(e.link))
		} else {
			w.Write([]byte(e.body))
		}
	}
	zw.Close()
	return buf.Bytes()
}

func writeTemp(t *testing.T, data []byte) string {
	p := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractTarGz(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	arc := writeTemp(t, tarGz(t, []entry{
		{name: "app-1.0/", typ: tar.TypeDir, mode: 0o755},
		{name: "app-1.0/app", body: string(elfBin), mode: 0o4755}, // setuid must go away
		{name: "app-1.0/README", body: "hi"},
		{name: "app-1.0/lib/libx.so.1", body: string(elfBin), mode: 0o755},
		{name: "app-1.0/lib/libx.so", typ: tar.TypeSymlink, link: "libx.so.1"},
		{name: "app-1.0/app-copy", typ: tar.TypeLink, link: "app-1.0/app"},
		{name: "app-1.0/fifo", typ: tar.TypeFifo},
	}))
	if err := Extract(arc, index.FormatTarGz, dest, ""); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dest, "app-1.0/app"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&fs.ModeSetuid != 0 || st.Mode().Perm() != 0o755 {
		t.Errorf("modo = %v", st.Mode())
	}
	if target, _ := os.Readlink(filepath.Join(dest, "app-1.0/lib/libx.so")); target != "libx.so.1" {
		t.Errorf("symlink interno = %q", target)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "app-1.0/app-copy")); !bytes.Equal(b, elfBin) {
		t.Error("hardlink not copied")
	}
	if _, err := os.Lstat(filepath.Join(dest, "app-1.0/fifo")); !os.IsNotExist(err) {
		t.Error("fifo should not be created")
	}
	exe, err := FindExecutable(dest, "app")
	if err != nil || exe != filepath.Join(dest, "app-1.0/app") {
		t.Errorf("exe = %q, %v", exe, err)
	}
}

func TestExtractRejectsUnsafe(t *testing.T) {
	cases := map[string][]entry{
		"zip slip ..":       {{name: "../evil", body: "x"}},
		"nested zip slip":   {{name: "a/../../evil", body: "x"}},
		"absolute path":     {{name: "/etc/evil", body: "x"}},
		"absolute symlink":  {{name: "link", typ: tar.TypeSymlink, link: "/etc/passwd"}},
		"escaping symlink":  {{name: "a/link", typ: tar.TypeSymlink, link: "../../x"}},
		"write via symlink": {{name: "d", typ: tar.TypeSymlink, link: "."}, {name: "d/x", body: "x"}},
		"hardlink outside":  {{name: "h", typ: tar.TypeLink, link: "../../etc/passwd"}},
		"backslash ..":      {{name: `..\evil`, body: "x"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "out")
			err := Extract(writeTemp(t, tarGz(t, entries)), index.FormatTarGz, dest, "")
			if !errors.Is(err, ErrUnsafePath) {
				t.Errorf("err = %v, want ErrUnsafePath", err)
			}
			if _, err := os.Stat(filepath.Join(root, "evil")); err == nil {
				t.Error("file escaped the destination")
			}
		})
	}
}

func TestExtractZip(t *testing.T) {
	dest := t.TempDir()
	arc := writeTemp(t, zipBytes(t, []entry{
		{name: "bin/tool", body: string(elfBin), mode: 0o755},
		{name: "bin/tool-link", link: "tool"},
	}))
	if err := Extract(arc, index.FormatZip, dest, ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "bin/tool")); !bytes.Equal(b, elfBin) {
		t.Error("wrong content")
	}

	for name, entries := range map[string][]entry{
		"zip slip":         {{name: "../../evil", body: "x"}},
		"escaping symlink": {{name: "l", link: "../../../etc/passwd"}},
		"absolute symlink": {{name: "l", link: "/etc/passwd"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := Extract(writeTemp(t, zipBytes(t, entries)), index.FormatZip, t.TempDir(), "")
			if !errors.Is(err, ErrUnsafePath) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestExtractPkgOnlyUsr(t *testing.T) {
	dest := t.TempDir()
	arc := writeTemp(t, tarZst(t, []entry{
		{name: ".PKGINFO", body: "pkgname = rawmakase"},
		{name: ".INSTALL", body: "post_install() { rm -rf ~; }"},
		{name: "etc/rawmakase.conf", body: "x"},
		{name: "usr/bin/rawmakase", body: string(elfBin), mode: 0o755},
		{name: "usr/share/icons/hicolor/scalable/apps/rawmakase.svg", body: "<svg/>"},
	}))
	if err := Extract(arc, index.FormatPkg, dest, ""); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{".PKGINFO", ".INSTALL", "etc"} {
		if _, err := os.Stat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s should not be extracted", gone)
		}
	}
	exe, err := FindExecutable(dest, "rawmakase")
	if err != nil || exe != filepath.Join(dest, "usr/bin/rawmakase") {
		t.Errorf("exe = %q %v", exe, err)
	}
}

func TestExtractBinary(t *testing.T) {
	dest := t.TempDir()
	if err := Extract(writeTemp(t, elfBin), index.FormatBinary, dest, "omavm"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dest, "omavm"))
	if st.Mode().Perm() != 0o755 {
		t.Errorf("modo = %v", st.Mode())
	}
	if err := Extract(writeTemp(t, elfBin), index.FormatBinary, dest, "../x"); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("malicious name: %v", err)
	}
}

func TestFindExecutable(t *testing.T) {
	mk := func(t *testing.T, files map[string][]byte, modes map[string]fs.FileMode) string {
		dir := t.TempDir()
		for n, b := range files {
			p := filepath.Join(dir, n)
			os.MkdirAll(filepath.Dir(p), 0o755)
			m := modes[n]
			if m == 0 {
				m = 0o755
			}
			os.WriteFile(p, b, m)
		}
		return dir
	}
	t.Run("prefers the repo name", func(t *testing.T) {
		dir := mk(t, map[string][]byte{"helper": elfBin, "bin/OmaPhoto": elfBin, "lib/omaphoto.so": elfBin}, nil)
		exe, _ := FindExecutable(dir, "omaphoto")
		if filepath.Base(exe) != "OmaPhoto" {
			t.Errorf("exe = %s", exe)
		}
	})
	t.Run("ELF without the x bit (Windows zip)", func(t *testing.T) {
		dir := mk(t, map[string][]byte{"tool": elfBin, "README.md": []byte("x")}, map[string]fs.FileMode{"tool": 0o644})
		exe, err := FindExecutable(dir, "other")
		if err != nil || filepath.Base(exe) != "tool" {
			t.Errorf("exe=%s err=%v", exe, err)
		}
	})
	t.Run("script without the x bit does not count", func(t *testing.T) {
		dir := mk(t, map[string][]byte{"run.sh": []byte("#!/bin/sh\n")}, map[string]fs.FileMode{"run.sh": 0o644})
		if _, err := FindExecutable(dir, "x"); !errors.Is(err, ErrNoExecutable) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("nothing executable", func(t *testing.T) {
		dir := mk(t, map[string][]byte{"LICENSE": []byte("MIT")}, nil)
		if _, err := FindExecutable(dir, "x"); !errors.Is(err, ErrNoExecutable) {
			t.Errorf("err = %v", err)
		}
	})
}
