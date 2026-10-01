// Package manifest reads the omastore.toml an app author puts at the root
// of the repository to declare what the heuristics would otherwise guess:
// name, summary, categories, icon, screenshots, terminal and, per
// architecture, which asset to install and which file is the executable.
//
// The content comes from untrusted repositories: every path is validated
// (relative, no "..") and every text has a bounded length. The manifest never
// widens what the installer accepts; it only chooses among already-safe options.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// FileName is the file name at the repository root.
const FileName = "omastore.toml"

// MaxSize limits the file size.
const MaxSize = 64 << 10

// Manifest is the content of omastore.toml. Every field is optional.
type Manifest struct {
	// Kind is the project type. Only "app" (the default) is indexed: OmaStore
	// does not distribute Omarchy plugins or themes.
	Kind        string   `toml:"kind" json:"kind,omitempty"`
	Name        string   `toml:"name" json:"name,omitempty"`
	Summary     string   `toml:"summary" json:"summary,omitempty"`
	Categories  []string `toml:"categories" json:"categories,omitempty"`
	Icon        string   `toml:"icon" json:"icon,omitempty"`
	Screenshots []string `toml:"screenshots" json:"screenshots,omitempty"`
	Terminal    *bool    `toml:"terminal" json:"terminal,omitempty"`
	// Linux maps an architecture (x86_64, aarch64 or the synonyms amd64,
	// arm64) to that architecture's asset and executable.
	Linux map[string]Target `toml:"linux" json:"linux,omitempty"`
}

// Target describes the installation on one architecture.
type Target struct {
	// Asset is the release asset name. Accepts {version} (tag without the
	// leading "v"), {tag} (tag as is) and * (any sequence).
	Asset string `toml:"asset" json:"asset,omitempty"`
	// Exec is the executable path inside the extracted package. Accepts
	// {version} and {tag}, like Asset (e.g. "app-{version}-x86_64-linux/bin/app").
	Exec string `toml:"exec" json:"exec,omitempty"`
}

// KindApp is the only indexed kind.
const KindApp = "app"

// IsApp reports whether the manifest declares an app (other kinds are not indexed).
func (m *Manifest) IsApp() bool { return m != nil && m.Kind == KindApp }

// Limits of the text fields.
const (
	maxName        = 80
	maxSummary     = 300
	maxCategories  = 4
	maxScreenshots = 8
	maxPath        = 256
)

// mainCategories are the freedesktop main categories.
var mainCategories = map[string]bool{
	"AudioVideo": true, "Audio": true, "Video": true, "Development": true, "Education": true,
	"Game": true, "Graphics": true, "Network": true, "Office": true, "Science": true,
	"Settings": true, "System": true, "Utility": true,
}

// additionalCategories are the freedesktop additional categories (Desktop
// Menu Specification, appendix A, plus the desktops desktop-file-validate
// registers). The reserved ones (Screensaver, TrayIcon, Applet, Shell) are
// left out: they need OnlyShowIn. They only go into the .desktop file.
var additionalCategories = map[string]bool{}

func init() {
	for _, c := range strings.Fields(`
		Building Debugger IDE GUIDesigner Profiling RevisionControl Translation
		Calendar ContactManagement Database Dictionary Chart Email Finance FlowChart PDA
		ProjectManagement Presentation Spreadsheet WordProcessor
		2DGraphics VectorGraphics RasterGraphics 3DGraphics Scanning OCR Photography
		Publishing Viewer TextTools DesktopSettings HardwareSettings Printing PackageManager
		Dialup InstantMessaging Chat IRCClient Feed FileTransfer HamRadio News P2P
		RemoteAccess Telephony TelephonyTools VideoConference WebBrowser WebDevelopment
		Midi Mixer Sequencer Tuner TV AudioVideoEditing Player Recorder DiscBurning
		ActionGame AdventureGame ArcadeGame BoardGame BlocksGame CardGame KidsGame
		LogicGame RolePlaying Shooter Simulation SportsGame StrategyGame
		Art Construction Music Languages ArtificialIntelligence Astronomy Biology
		Chemistry ComputerScience DataVisualization Economy Electricity Geography Geology
		Geoscience History Humanities ImageProcessing Literature Maps Math
		NumericalAnalysis MedicalSoftware Physics Robotics Spirituality Sports
		ParallelComputing Amusement Archiving Compression Electronics Emulator
		Engineering FileTools FileManager TerminalEmulator Filesystem Monitor Security
		Accessibility Calculator Clock TextEditor Documentation Adult Core
		KDE GNOME XFCE DDE LXQt COSMIC GTK Qt Motif Java ConsoleOnly`) {
		additionalCategories[c] = true
	}
}

// reExtension accepts the specification's own extensions ("X-Omarchy").
var reExtension = regexp.MustCompile(`^X-[A-Za-z0-9-]{1,38}$`)

// validCategory reports whether c is a registered freedesktop category or an
// X- extension.
func validCategory(c string) bool {
	return mainCategories[c] || additionalCategories[c] || reExtension.MatchString(c)
}

var archAliases = map[string]string{
	"x86_64": "amd64", "amd64": "amd64", "x64": "amd64",
	"aarch64": "arm64", "arm64": "arm64",
}

// NormArch converts a manifest architecture name into its GOARCH name.
func NormArch(a string) (string, bool) {
	n, ok := archAliases[strings.ToLower(a)]
	return n, ok
}

// Problem is an error or warning found while validating.
type Problem struct {
	Field   string
	Message string
	Warning bool
}

func (p Problem) String() string {
	kind := "error"
	if p.Warning {
		kind = "warning"
	}
	return fmt.Sprintf("%s: %s: %s", kind, p.Field, p.Message)
}

// Parse reads and validates a manifest. strict rejects unknown fields (useful
// for linting; the indexer accepts new fields to stay compatible).
// Invalid fields are removed from the result and reported in problems;
// err is only returned if the file cannot be read as TOML.
func Parse(data []byte, strict bool) (*Manifest, []Problem, error) {
	if len(data) > MaxSize {
		return nil, nil, fmt.Errorf("%s larger than %d bytes", FileName, MaxSize)
	}
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(data))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(&m); err != nil {
		var sm *toml.StrictMissingError
		if errors.As(err, &sm) {
			return nil, []Problem{{Field: "(file)", Message: "unknown fields:\n" + sm.String()}}, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", FileName, err)
	}
	problems := m.sanitize()
	return &m, problems, nil
}

// cleanText normalizes whitespace and limits the length.
func cleanText(s string, max int) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	if !utf8.ValidString(s) {
		return "", false
	}
	if utf8.RuneCountInString(s) > max {
		return string([]rune(s)[:max]), false
	}
	return s, true
}

// relPath validates a relative path inside the repository/package.
func relPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty")
	}
	if len(p) > maxPath {
		return "", errors.New("too long")
	}
	if strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", errors.New("must be relative, with /")
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", errors.New("leaves the repository")
	}
	return c, nil
}

// isIconPath accepts the formats the hicolor theme supports.
func isIconPath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".svg":
		return true
	}
	return false
}

func isImagePath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".svg", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	}
	return false
}

// sanitize removes invalid values and returns the problems found.
func (m *Manifest) sanitize() []Problem {
	var ps []Problem
	errf := func(field, format string, a ...any) {
		ps = append(ps, Problem{Field: field, Message: fmt.Sprintf(format, a...)})
	}
	warnf := func(field, format string, a ...any) {
		ps = append(ps, Problem{Field: field, Message: fmt.Sprintf(format, a...), Warning: true})
	}

	switch k := strings.ToLower(strings.TrimSpace(m.Kind)); k {
	case "", KindApp:
		m.Kind = KindApp
	case "plugin", "theme":
		m.Kind = k
		warnf("kind", "%q: OmaStore only indexes apps; this repository is not added to the catalog", k)
	default:
		errf("kind", "%q is unknown (use \"app\")", m.Kind)
		m.Kind = k
	}

	if m.Name != "" {
		n, ok := cleanText(m.Name, maxName)
		if !ok {
			warnf("name", "truncated to %d characters", maxName)
		}
		m.Name = n
	}
	if m.Summary != "" {
		s, ok := cleanText(m.Summary, maxSummary)
		if !ok {
			warnf("summary", "truncated to %d characters", maxSummary)
		}
		m.Summary = s
	}

	var cats []string
	seen := map[string]bool{}
	for _, c := range m.Categories {
		c = strings.TrimSpace(c)
		switch {
		case seen[c]:
			continue
		case !validCategory(c):
			errf("categories", "%q is not a freedesktop category (see the Desktop Menu Specification; extensions start with X-)", c)
			continue
		case len(cats) == maxCategories:
			warnf("categories", "only the first %d are used", maxCategories)
			continue
		}
		seen[c] = true
		cats = append(cats, c)
	}
	m.Categories = cats
	if len(cats) > 0 && m.MainCategory() == "" {
		// Without a main category the app does not show up in any menu.
		warnf("categories", "no freedesktop main category (e.g. Graphics, System)")
	}

	if m.Icon != "" {
		p, err := relPath(m.Icon)
		switch {
		case err != nil:
			errf("icon", "%q: %v", m.Icon, err)
			p = ""
		case !isIconPath(p):
			errf("icon", "%q: use PNG or SVG", m.Icon)
			p = ""
		}
		m.Icon = p
	}

	var shots []string
	for _, s := range m.Screenshots {
		if strings.HasPrefix(s, "https://") {
			shots = append(shots, s)
		} else if p, err := relPath(s); err != nil {
			errf("screenshots", "%q: %v", s, err)
			continue
		} else if !isImagePath(p) {
			errf("screenshots", "%q is not an image", s)
			continue
		} else {
			shots = append(shots, p)
		}
		if len(shots) == maxScreenshots {
			if len(m.Screenshots) > maxScreenshots {
				warnf("screenshots", "only the first %d are used", maxScreenshots)
			}
			break
		}
	}
	m.Screenshots = shots

	targets := map[string]Target{}
	keys := make([]string, 0, len(m.Linux))
	for k := range m.Linux {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := m.Linux[k]
		arch, ok := NormArch(k)
		field := "linux." + k
		if !ok {
			errf(field, "unknown architecture (use x86_64 or aarch64)")
			continue
		}
		if _, dup := targets[arch]; dup {
			errf(field, "duplicated architecture")
			continue
		}
		if t.Asset != "" {
			if err := checkPattern(t.Asset); err != nil {
				errf(field+".asset", "%v", err)
				t.Asset = ""
			}
		}
		if t.Exec != "" {
			p, err := relPath(t.Exec)
			if err == nil {
				err = checkPlaceholders(t.Exec)
			}
			if err != nil {
				errf(field+".exec", "%q: %v", t.Exec, err)
				p = ""
			}
			t.Exec = p
		}
		if t.Asset == "" && t.Exec == "" {
			continue
		}
		targets[arch] = t
	}
	m.Linux = targets
	if len(m.Linux) == 0 {
		m.Linux = nil
	}
	return ps
}

// MainCategory is the first declared main category, or "".
func (m *Manifest) MainCategory() string {
	for _, c := range m.Categories {
		if mainCategories[c] {
			return c
		}
	}
	return ""
}

// Target returns the target for an architecture (GOARCH name).
func (m *Manifest) Target(goarch string) (Target, bool) {
	if m == nil {
		return Target{}, false
	}
	t, ok := m.Linux[goarch]
	return t, ok
}

// Empty reports whether the manifest declares nothing.
func (m *Manifest) Empty() bool {
	return m == nil || ((m.Kind == "" || m.Kind == KindApp) && m.Name == "" && m.Summary == "" && len(m.Categories) == 0 && m.Icon == "" &&
		len(m.Screenshots) == 0 && m.Terminal == nil && len(m.Linux) == 0)
}

var rePlaceholder = regexp.MustCompile(`\{[^}]*\}`)

func checkPattern(p string) error {
	if len(p) > maxPath {
		return errors.New("pattern too long")
	}
	if strings.ContainsAny(p, "/\\") {
		return errors.New("the asset name cannot contain /")
	}
	if err := checkPlaceholders(p); err != nil {
		return err
	}
	if strings.Trim(p, "*") == "" {
		return errors.New("pattern too generic")
	}
	return nil
}

func checkPlaceholders(p string) error {
	for _, ph := range rePlaceholder.FindAllString(p, -1) {
		if ph != "{version}" && ph != "{tag}" {
			return fmt.Errorf("unknown placeholder %s (use {version} or {tag})", ph)
		}
	}
	return nil
}

// Expand replaces {version} (tag without "v") and {tag} with the release values.
func Expand(pattern, tag string) string {
	version := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	return strings.NewReplacer("{version}", version, "{tag}", tag).Replace(pattern)
}

// MatchAsset reports whether the asset name matches the pattern for the given tag.
func MatchAsset(pattern, tag, name string) bool {
	return globMatch(Expand(pattern, tag), name)
}

// globMatch matches * against any sequence (no other metacharacters).
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// Encode serializes the (already validated) manifest for the database. An
// empty manifest becomes "{...kind...}", never "": the presence of the file is
// what enables indexing.
func (m *Manifest) Encode() string {
	if m == nil {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// Decode reads what Encode wrote. An empty or invalid value becomes nil.
func Decode(s string) *Manifest {
	if s == "" {
		return nil
	}
	var m Manifest
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return &m
}
