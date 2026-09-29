package index

import (
	"math"
	"strings"
	"time"
)

// Categorias principais da freedesktop Desktop Menu Specification, usadas
// tanto no catálogo quanto no campo Categories do .desktop.
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

// topicCategories mapeia palavras de topics para categorias. A ordem importa:
// a primeira categoria com correspondência vence.
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

// Category deriva a categoria a partir dos topics (e, na falta, do nome e da
// descrição). O default é Utility.
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

// recencyHalfLife controla quão rápido a recência perde peso.
const recencyHalfLife = 180 * 24 * time.Hour

// Score ordena o catálogo: stars primeiro, recência como desempate. A parte
// fracionária (0..1) vem da recência, então nunca supera uma star a mais.
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
