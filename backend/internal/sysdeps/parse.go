// Package sysdeps reads the system dependencies (depends and optdepends) an
// app declares in its repository's PKGBUILD or .SRCINFO, and checks and
// installs the missing ones through pacman.
//
// The PKGBUILD is never executed: only literal array assignments are read,
// and elements with expansions ($var, $(cmd), `cmd`) are dropped.
package sysdeps

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Dep is a declared dependency.
type Dep struct {
	// Spec is the dependency as declared, with an optional version
	// constraint ("qt6-base>=6.5").
	Spec string `json:"spec"`
	// Reason is the optdepends description ("" for depends).
	Reason   string `json:"reason,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

// Name is the package name without the version constraint.
func (d Dep) Name() string { return nameOf(d.Spec) }

// Set is what an app declares.
type Set struct {
	Source string `json:"source"` // repository path it was read from
	Deps   []Dep  `json:"deps"`
}

// MaxFileSize limits the PKGBUILD/.SRCINFO read from a repository.
const MaxFileSize = 64 << 10

// maxDeps limits how many dependencies are kept from one file.
const maxDeps = 64

// namePattern is a pacman package name (makepkg's rules), optionally
// followed by a version constraint.
var (
	namePattern = regexp.MustCompile(`^[a-z0-9@_+][a-z0-9@._+-]*$`)
	specPattern = regexp.MustCompile(`^([a-z0-9@_+][a-z0-9@._+-]*)((?:[<>]=?|=)[A-Za-z0-9:._+~-]+)?$`)
)

// ValidName reports whether s is a valid pacman package name.
func ValidName(s string) bool { return len(s) <= 128 && namePattern.MatchString(s) }

// validSpec reports whether s is a package name with an optional constraint.
func validSpec(s string) bool { return len(s) <= 160 && specPattern.MatchString(s) }

func nameOf(spec string) string {
	if i := strings.IndexAny(spec, "<>="); i >= 0 {
		return spec[:i]
	}
	return spec
}

// Arch maps a Go architecture to pacman's (for depends_<arch>).
func Arch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	}
	return goarch
}

// Candidates picks the files to read from a repository listing, best first:
// a .SRCINFO before the PKGBUILD in the same directory, directories of
// binary packages (-bin) before others, shallower before deeper.
func Candidates(files []string) []string {
	type cand struct {
		path  string
		score int
	}
	var cs []cand
	for _, f := range files {
		base := path.Base(f)
		if base != "PKGBUILD" && base != ".SRCINFO" {
			continue
		}
		dir := path.Dir(f)
		depth := 0
		if dir != "." {
			depth = strings.Count(dir, "/") + 1
		}
		if depth > 3 || skippedDir(dir) {
			continue
		}
		score := depth * 10
		if base == "PKGBUILD" {
			score++
		}
		if !strings.Contains(strings.ToLower(dir), "bin") {
			score += 5
		}
		cs = append(cs, cand{f, score})
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score < cs[j].score
		}
		return cs[i].path < cs[j].path
	})
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.path)
	}
	return out
}

func skippedDir(dir string) bool {
	for _, part := range strings.Split(strings.ToLower(dir), "/") {
		switch part {
		case "test", "tests", "testdata", "vendor", "node_modules", "examples", "third_party":
			return true
		}
	}
	return false
}

// Parse reads a PKGBUILD or .SRCINFO (by the base name of file). pkg picks
// the package of a split PKGBUILD whose name starts with it (usually the
// repository name); goarch selects depends_<arch>.
func Parse(file, content, pkg, goarch string) Set {
	var fields packageFields
	if path.Base(file) == ".SRCINFO" {
		fields = parseSRCINFO(content, pkg)
	} else {
		fields = parsePKGBUILD(content, pkg)
	}
	arch := Arch(goarch)
	var deps []Dep
	seen := map[string]bool{}
	add := func(spec, reason string, optional bool) {
		spec = strings.TrimSpace(spec)
		if !validSpec(spec) || seen[nameOf(spec)] || len(deps) >= maxDeps {
			return
		}
		seen[nameOf(spec)] = true
		deps = append(deps, Dep{Spec: spec, Reason: strings.TrimSpace(reason), Optional: optional})
	}
	for _, key := range []string{"depends", "depends_" + arch} {
		for _, v := range fields[key] {
			add(v, "", false)
		}
	}
	for _, key := range []string{"optdepends", "optdepends_" + arch} {
		for _, v := range fields[key] {
			spec, reason, _ := strings.Cut(v, ":")
			add(spec, reason, true)
		}
	}
	return Set{Source: file, Deps: deps}
}

// packageFields maps an array name (depends, optdepends_x86_64...) to its values.
type packageFields map[string][]string

func wanted(key string) bool {
	return key == "depends" || key == "optdepends" ||
		strings.HasPrefix(key, "depends_") || strings.HasPrefix(key, "optdepends_")
}

// merge applies a package section over the base: a key the package sets
// replaces the base's (that is how makepkg treats split packages).
func merge(base, pkg packageFields) packageFields {
	out := packageFields{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range pkg {
		out[k] = v
	}
	return out
}

// pickPackage chooses the package section: the one named pkg, then one that
// starts with it (pkg-bin, pkg-git), then the first.
func pickPackage(names []string, pkg string) string {
	pkg = strings.ToLower(pkg)
	for _, n := range names {
		if n == pkg {
			return n
		}
	}
	for _, n := range names {
		if strings.HasPrefix(n, pkg+"-") {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// parseSRCINFO reads the "key = value" lines of a .SRCINFO.
func parseSRCINFO(content, pkg string) packageFields {
	base := packageFields{}
	pkgs := map[string]packageFields{}
	var names []string
	cur := base
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "pkgbase":
			cur = base
			continue
		case "pkgname":
			if _, dup := pkgs[val]; !dup {
				names = append(names, val)
				pkgs[val] = packageFields{}
			}
			cur = pkgs[val]
			continue
		}
		if !wanted(key) {
			continue
		}
		// An empty value clears the base's list for this package.
		if _, set := cur[key]; !set {
			cur[key] = []string{}
		}
		if val != "" {
			cur[key] = append(cur[key], val)
		}
	}
	if name := pickPackage(names, pkg); name != "" {
		return merge(base, pkgs[name])
	}
	return base
}

// Function headers of a PKGBUILD: package() and package_<name>() hold the
// arrays of split packages; any other function body is ignored.
var (
	funcPattern   = regexp.MustCompile(`^\s*(?:function\s+)?([A-Za-z_][A-Za-z0-9_@.+-]*)\s*\(\s*\)\s*\{?\s*$`)
	assignPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)(\+?=)\(`)
)

// parsePKGBUILD reads the literal array assignments of a PKGBUILD.
func parsePKGBUILD(content, pkg string) packageFields {
	base := packageFields{}
	pkgs := map[string]packageFields{}
	var names []string
	var cur packageFields = base
	inFunc := false
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if m := funcPattern.FindStringSubmatch(line); m != nil {
			inFunc = true
			cur = nil
			if name, ok := strings.CutPrefix(m[1], "package_"); ok {
				names = append(names, name)
				pkgs[name] = packageFields{}
				cur = pkgs[name]
			} else if m[1] == "package" {
				cur = pkgs[""]
				if cur == nil {
					cur = packageFields{}
					pkgs[""] = cur
				}
			}
			continue
		}
		if inFunc && strings.HasPrefix(line, "}") {
			inFunc, cur = false, base
			continue
		}
		m := assignPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// The array may span lines: read until its closing parenthesis.
		rest := line[len(m[0]):]
		values, closed := splitArray(rest)
		for !closed && i+1 < len(lines) {
			i++
			rest += "\n" + lines[i]
			values, closed = splitArray(rest)
		}
		if cur == nil || !wanted(m[1]) {
			continue
		}
		if m[2] == "=" {
			cur[m[1]] = values
		} else {
			cur[m[1]] = append(cur[m[1]], values...)
		}
	}
	fields := base
	if p, ok := pkgs[""]; ok {
		fields = merge(fields, p)
	}
	if name := pickPackage(names, pkg); name != "" {
		fields = merge(fields, pkgs[name])
	}
	return fields
}

// splitArray splits the inside of a bash array literal, up to the closing
// parenthesis. Elements with expansions are dropped: their value is only
// known by running the script.
func splitArray(s string) (values []string, closed bool) {
	var (
		cur       strings.Builder
		inToken   bool
		dynamic   bool
		quote     byte
		inComment bool
	)
	flush := func() {
		if inToken && !dynamic {
			values = append(values, cur.String())
		}
		cur.Reset()
		inToken, dynamic = false, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case quote == '"':
			switch c {
			case '"':
				quote = 0
			case '$', '`':
				dynamic = true
			case '\\':
				if i+1 < len(s) {
					i++
					cur.WriteByte(s[i])
				}
			default:
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, inToken = c, true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case c == '#' && !inToken:
			inComment = true
		case c == ')':
			flush()
			return values, true
		case c == '\\':
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
				continue
			}
			inToken = true
			if i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			}
		default:
			if c == '$' || c == '`' || c == '(' || c == '{' || c == '*' || c == '?' || c == '[' {
				dynamic = true
			}
			inToken = true
			cur.WriteByte(c)
		}
	}
	return values, false
}
