// Package elfdeps finds the shared libraries an installed executable needs and
// this system does not have, by reading its ELF headers. Nothing is run (not
// even ldd, which may execute the program): only debug/elf and the linker's
// configuration files are read.
package elfdeps

import (
	"bufio"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ErrNotELF means the executable is not an ELF file (a script, an
// interpreter launcher): there is nothing to check.
var ErrNotELF = errors.New("not an ELF executable")

// Result is what a check found.
type Result struct {
	// WrongArch is set when the executable was built for another machine
	// (Machine names it), e.g. an aarch64 file in an x86_64 tarball.
	WrongArch bool
	Machine   string
	// Missing are the needed libraries found neither in the app's own files
	// nor in the system's library directories, sorted.
	Missing []string
}

// machines maps GOARCH to the ELF machine it runs.
var machines = map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}

// Check reads exe and the libraries it brings in root (the app's version
// directory), and reports the needed libraries that resolve nowhere.
// systemDirs are the system's library directories (see SystemDirs).
func Check(exe, root string, systemDirs []string) (Result, error) {
	f, err := elf.Open(exe)
	if err != nil {
		var fe *elf.FormatError
		if errors.As(err, &fe) {
			return Result{}, ErrNotELF
		}
		return Result{}, err
	}
	defer f.Close()

	var res Result
	if want, ok := machines[runtime.GOARCH]; ok && f.Machine != want {
		res.WrongArch, res.Machine = true, strings.TrimPrefix(f.Machine.String(), "EM_")
		return res, nil
	}
	if f.Class == elf.ELFCLASS32 {
		// 32-bit libraries live elsewhere (lib32); not worth guessing.
		return res, nil
	}

	bundled := bundledLibs(root)
	missing := map[string]bool{}
	seen := map[string]bool{}
	// The executable, then each bundled library it pulls in (their own
	// needs count too); system libraries are trusted to be complete.
	queue := []string{exe}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		g, err := elf.Open(path)
		if err != nil {
			continue
		}
		needed, _ := g.ImportedLibraries()
		dirs := origins(g, path)
		g.Close()
		for _, lib := range needed {
			if seen[lib] {
				continue
			}
			seen[lib] = true
			if p := findIn(lib, dirs); p != "" {
				if within(root, p) {
					queue = append(queue, p)
				}
				continue
			}
			if p, ok := bundled[lib]; ok {
				queue = append(queue, p)
				continue
			}
			if findIn(lib, systemDirs) != "" {
				continue
			}
			missing[lib] = true
		}
	}
	for lib := range missing {
		res.Missing = append(res.Missing, lib)
	}
	sort.Strings(res.Missing)
	return res, nil
}

// origins are the directories an ELF file asks the loader to search first:
// its RUNPATH/RPATH ($ORIGIN expanded) and its own directory.
func origins(f *elf.File, path string) []string {
	dir := filepath.Dir(path)
	var out []string
	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		vals, _ := f.DynString(tag)
		for _, v := range vals {
			for _, p := range strings.Split(v, ":") {
				p = strings.NewReplacer("$ORIGIN", dir, "${ORIGIN}", dir).Replace(p)
				if filepath.IsAbs(p) {
					out = append(out, filepath.Clean(p))
				}
			}
		}
	}
	return append(out, dir)
}

// bundledLibs indexes the shared libraries shipped inside root by file name
// (apps often keep them in lib/ and set LD_LIBRARY_PATH in a wrapper).
func bundledLibs(root string) map[string]string {
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".so") {
			if _, ok := out[d.Name()]; !ok {
				out[d.Name()] = p
			}
		}
		return nil
	})
	return out
}

func findIn(lib string, dirs []string) string {
	for _, d := range dirs {
		p := filepath.Join(d, lib)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// SystemDirs are the directories the dynamic loader searches: the ones in
// /etc/ld.so.conf (following its includes) and the default ones.
func SystemDirs() []string {
	return systemDirs("/etc/ld.so.conf")
}

func systemDirs(conf string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		d = filepath.Clean(d)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	readConf(conf, add, 0)
	for _, d := range []string{"/usr/lib", "/usr/lib64", "/lib", "/lib64", "/usr/local/lib"} {
		add(d)
	}
	return dirs
}

func readConf(path string, add func(string), depth int) {
	if depth > 4 {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "include "):
			pattern := strings.TrimSpace(strings.TrimPrefix(line, "include "))
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(filepath.Dir(path), pattern)
			}
			matches, _ := filepath.Glob(pattern)
			sort.Strings(matches)
			for _, m := range matches {
				readConf(m, add, depth+1)
			}
		case filepath.IsAbs(line):
			add(line)
		}
	}
}

// String is a short description for logs.
func (r Result) String() string {
	if r.WrongArch {
		return fmt.Sprintf("built for %s", r.Machine)
	}
	return fmt.Sprintf("missing %v", r.Missing)
}
