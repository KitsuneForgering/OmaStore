package install

import (
	"strings"
	"unicode"
)

// escapeValue escapes a Desktop Entry Specification string/localestring
// value: a backslash becomes "\\" and control characters (including line
// breaks) become spaces, so no field coming from the repository can inject
// new keys into the file.
func escapeValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case unicode.IsControl(r) || r == ' ' || r == ' ':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// execReserved are the characters that force quoting an Exec
// argument.
const execReserved = " \t\n\"'\\><~|&;$*?#()`"

// quoteExecArg applies the Exec field quoting rules to an argument. The
// result still has to go through escapeValue (the specification applies the
// string escape before the quoting when reading).
func quoteExecArg(arg string) string {
	arg = strings.ReplaceAll(arg, "%", "%%")
	if !strings.ContainsAny(arg, execReserved) {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range arg {
		if r == '"' || r == '`' || r == '$' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// Desktop holds the fields of the generated .desktop file.
type Desktop struct {
	Name     string
	Comment  string
	Exec     string // absolute path of the executable
	Icon     string // icon name in the theme or absolute path
	Terminal bool
	// TUILauncher is Omarchy's omarchy-launch-or-focus-tui (absolute path), or
	// "" outside Omarchy. With it, a terminal app opens in the configured
	// terminal, and focuses the window it already has, as AppID.
	TUILauncher string
	AppID       string
	Categories  []string
	Repo        string
	Version     string
}

// Render generates the .desktop content.
func (d Desktop) Render() string {
	var b strings.Builder
	line := func(k, v string) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	b.WriteString("[Desktop Entry]\n")
	line("Type", "Application")
	line("Version", "1.5")
	name := escapeValue(d.Name)
	if name == "" {
		name = escapeValue(d.Repo)
	}
	line("Name", name)
	if c := escapeValue(d.Comment); c != "" {
		line("Comment", c)
	}
	terminal := d.Terminal
	if terminal && d.TUILauncher != "" && shellSafe(d.TUILauncher) && shellSafe(d.Exec) && shellSafe(d.AppID) {
		line("Exec", d.TUILauncher+" --app-id="+d.AppID+" "+d.Exec)
		terminal = false
	} else {
		line("Exec", escapeValue(quoteExecArg(d.Exec)))
	}
	line("TryExec", escapeValue(d.Exec))
	if d.Icon != "" {
		line("Icon", escapeValue(d.Icon))
	}
	if terminal {
		line("Terminal", "true")
	} else {
		line("Terminal", "false")
	}
	var cats []string
	for _, c := range d.Categories {
		if c = sanitizeListItem(c); c != "" {
			cats = append(cats, c)
		}
	}
	if len(cats) > 0 {
		line("Categories", strings.Join(cats, ";")+";")
	}
	line("X-OmaStore-Repo", escapeValue(d.Repo))
	line("X-OmaStore-Version", escapeValue(d.Version))
	return b.String()
}

// shellSafe reports whether s holds only characters that need no quoting in a
// shell or in the Exec key. omarchy-launch-or-focus-tui joins its arguments
// into a string that omarchy-launch-or-focus runs with eval, so anything else
// (a space in $HOME, a quote, a '$') keeps the plain Terminal=true entry.
func shellSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._/+-", r))) {
			return false
		}
	}
	return true
}

// sanitizeListItem keeps only safe characters in a list item.
func sanitizeListItem(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return r
		}
		return -1
	}, s)
}

// terminalTopics mark a terminal app (CLI/TUI).
var terminalTopics = map[string]bool{"cli": true, "tui": true, "terminal": true, "command-line": true, "console": true}

// isTerminalApp decides the Terminal field from the topics.
func isTerminalApp(topics []string) bool {
	for _, t := range topics {
		if terminalTopics[strings.ToLower(t)] {
			return true
		}
	}
	return false
}
