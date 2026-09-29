package install

import (
	"strings"
	"unicode"
)

// escapeValue escapa um valor do tipo string/localestring da Desktop Entry
// Specification: barra invertida vira "\\" e caracteres de controle (quebras
// de linha inclusive) viram espaço, para que nenhum campo vindo do
// repositório consiga injetar chaves novas no arquivo.
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

// execReserved são os caracteres que obrigam a pôr um argumento de Exec
// entre aspas.
const execReserved = " \t\n\"'\\><~|&;$*?#()`"

// quoteExecArg aplica as regras de aspas do campo Exec a um argumento. O
// resultado ainda precisa passar por escapeValue (a especificação aplica o
// escape de string antes das aspas na leitura).
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

// Desktop são os campos do arquivo .desktop gerado.
type Desktop struct {
	Name       string
	Comment    string
	Exec       string // caminho absoluto do executável
	Icon       string // nome do ícone no tema ou caminho absoluto
	Terminal   bool
	Categories []string
	Repo       string
	Version    string
}

// Render gera o conteúdo do .desktop.
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
	line("Exec", escapeValue(quoteExecArg(d.Exec)))
	line("TryExec", escapeValue(d.Exec))
	if d.Icon != "" {
		line("Icon", escapeValue(d.Icon))
	}
	if d.Terminal {
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

// sanitizeListItem mantém só caracteres seguros num item de lista.
func sanitizeListItem(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return r
		}
		return -1
	}, s)
}

// terminalTopics indicam um app de terminal (CLI/TUI).
var terminalTopics = map[string]bool{"cli": true, "tui": true, "terminal": true, "command-line": true, "console": true}

// isTerminalApp decide o campo Terminal a partir dos topics.
func isTerminalApp(topics []string) bool {
	for _, t := range topics {
		if terminalTopics[strings.ToLower(t)] {
			return true
		}
	}
	return false
}
