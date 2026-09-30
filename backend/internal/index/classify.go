package index

import (
	"math"
	"strings"
	"time"
)

// Main categories of the freedesktop Desktop Menu Specification, used both
// in the catalog and in the .desktop Categories field.
const (
	CatAudioVideo  = "AudioVideo"
	CatDevelopment = "Development"
	CatEducation   = "Education"
	CatGame        = "Game"
	CatGraphics    = "Graphics"
	CatNetwork     = "Network"
	CatOffice      = "Office"
	CatScience     = "Science"
	CatSettings    = "Settings"
	CatSystem      = "System"
	CatUtility     = "Utility"
)

// topicCategories maps topic words to categories. The order matters:
// the first category that matches wins.
var topicCategories = []struct {
	cat   string
	words []string
}{
	{CatGame, []string{"game", "games", "gaming", "emulator", "steam"}},
	{CatAudioVideo, []string{"audio", "music", "video", "player", "media", "podcast", "sound", "recorder",
		"recording", "streaming", "whisper", "speech", "voice", "transcription", "stt", "tts", "pipewire", "mpv"}},
	{CatGraphics, []string{"photo", "photography", "image", "images", "graphics", "design", "drawing",
		"paint", "raw", "camera", "screenshot", "annotation", "annotations", "whiteboard", "svg", "color", "wallpaper", "icons",
		"lightroom", "darktable", "photoshop", "gimp", "illustrator", "inkscape", "krita", "figma"}},
	{CatDevelopment, []string{"development", "developer-tools", "devtools", "ide", "editor", "git",
		"debugger", "compiler", "api", "database", "sql", "terminal-emulator", "code"}},
	{CatNetwork, []string{"network", "networking", "browser", "email", "mail", "chat", "irc", "matrix",
		"vpn", "wifi", "bluetooth", "ssh", "download", "torrent", "rss", "web"}},
	{CatOffice, []string{"office", "notes", "note-taking", "markdown", "pdf", "calendar", "todo",
		"productivity", "documents", "spreadsheet", "writing", "meeting"}},
	{CatEducation, []string{"education", "learning", "flashcards"}},
	{CatScience, []string{"science", "math", "astronomy", "chemistry"}},
	{CatSystem, []string{"system", "vm", "virtualization", "qemu", "kvm", "virtual-machine", "monitor",
		"monitoring", "hyprland", "wayland", "display", "disk", "backup", "security", "containers", "docker"}},
	{CatSettings, []string{"settings", "config", "configuration", "theme", "themes", "dotfiles"}},
}

// Category derives the category from the topics (and, failing that, from the
// name and description). The default is Utility.
func Category(topics []string, name, description string) string {
	set := map[string]bool{}
	for _, t := range topics {
		set[strings.ToLower(t)] = true
	}
	for _, tc := range topicCategories {
		for _, w := range tc.words {
			if set[w] {
				return tc.cat
			}
		}
	}
	text := " " + strings.ToLower(name+" "+description) + " "
	text = strings.NewReplacer(",", " ", ".", " ", "(", " ", ")", " ", "-", " ").Replace(text)
	for _, tc := range topicCategories {
		for _, w := range tc.words {
			if len(w) >= 4 && strings.Contains(text, " "+w+" ") {
				return tc.cat
			}
		}
	}
	return CatUtility
}

// recencyHalfLife controls how fast recency loses weight.
const recencyHalfLife = 180 * 24 * time.Hour

// Score orders the catalog: stars first, recency as a tiebreaker. The
// fractional part (0..1) comes from recency, so it never beats one more star.
func Score(stars int, pushedAt, now time.Time) float64 {
	rec := 0.0
	if !pushedAt.IsZero() {
		age := now.Sub(pushedAt)
		if age < 0 {
			age = 0
		}
		rec = math.Pow(0.5, float64(age)/float64(recencyHalfLife))
	}
	return float64(stars) + 0.999*rec
}
