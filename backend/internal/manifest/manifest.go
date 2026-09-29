// Package manifest lê o omastore.toml opcional que o autor de um app coloca
// na raiz do repositório para declarar o que as heurísticas deduziriam:
// nome, resumo, categorias, ícone, screenshots, terminal e, por
// arquitetura, qual asset instalar e qual é o executável.
//
// O conteúdo vem de repositórios não confiáveis: todo caminho é validado
// (relativo, sem "..") e todo texto tem tamanho limitado. O manifesto nunca
// amplia o que o instalador aceita; só escolhe entre opções já seguras.
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

// FileName é o nome do arquivo na raiz do repositório.
const FileName = "omastore.toml"

// MaxSize limita o tamanho do arquivo.
const MaxSize = 64 << 10

// Manifest é o conteúdo do omastore.toml. Todos os campos são opcionais.
type Manifest struct {
	// Kind é o tipo do projeto. Só "app" (o padrão) é indexado: a OmaStore
	// não distribui plugins nem temas do Omarchy.
	Kind        string   `toml:"kind" json:"kind,omitempty"`
	Name        string   `toml:"name" json:"name,omitempty"`
	Summary     string   `toml:"summary" json:"summary,omitempty"`
	Categories  []string `toml:"categories" json:"categories,omitempty"`
	Icon        string   `toml:"icon" json:"icon,omitempty"`
	Screenshots []string `toml:"screenshots" json:"screenshots,omitempty"`
	Terminal    *bool    `toml:"terminal" json:"terminal,omitempty"`
	// Linux mapeia arquitetura (x86_64, aarch64 ou os sinônimos amd64,
	// arm64) para o asset e o executável daquela arquitetura.
	Linux map[string]Target `toml:"linux" json:"linux,omitempty"`
}

// Target descreve a instalação numa arquitetura.
type Target struct {
	// Asset é o nome do asset da release. Aceita {version} (tag sem o "v"
	// inicial), {tag} (tag como está) e * (qualquer sequência).
	Asset string `toml:"asset" json:"asset,omitempty"`
	// Exec é o caminho do executável dentro do pacote extraído. Aceita
	// {version} e {tag}, como Asset (ex.: "app-{version}-x86_64-linux/bin/app").
	Exec string `toml:"exec" json:"exec,omitempty"`
}

// KindApp é o único tipo indexado.
const KindApp = "app"

// IsApp diz se o manifesto declara um app (os demais tipos não são indexados).
func (m *Manifest) IsApp() bool { return m != nil && m.Kind == KindApp }

// Limites dos campos de texto.
const (
	maxName        = 80
	maxSummary     = 300
	maxCategories  = 4
	maxScreenshots = 8
	maxPath        = 256
)

// mainCategories são as categorias principais da freedesktop.
var mainCategories = map[string]bool{
	"AudioVideo": true, "Audio": true, "Video": true, "Development": true, "Education": true,
	"Game": true, "Graphics": true, "Network": true, "Office": true, "Science": true,
	"Settings": true, "System": true, "Utility": true,
}

// additionalCategories aceitas além das principais (subconjunto comum da
// especificação); servem só para o .desktop.
var reAdditional = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{1,39}$`)

var archAliases = map[string]string{
	"x86_64": "amd64", "amd64": "amd64", "x64": "amd64",
	"aarch64": "arm64", "arm64": "arm64",
}

// NormArch converte o nome de arquitetura do manifesto para o do GOARCH.
func NormArch(a string) (string, bool) {
	n, ok := archAliases[strings.ToLower(a)]
	return n, ok
}

// Problem é um erro ou aviso encontrado ao validar.
type Problem struct {
	Field   string
	Message string
	Warning bool
}

func (p Problem) String() string {
	kind := "erro"
	if p.Warning {
		kind = "aviso"
	}
	return fmt.Sprintf("%s: %s: %s", kind, p.Field, p.Message)
}

// Parse lê e valida um manifesto. strict rejeita campos desconhecidos (útil
// no lint; o indexador aceita campos novos para manter compatibilidade).
// Campos inválidos são removidos do resultado e reportados em problems;
// err só é retornado se o arquivo não puder ser lido como TOML.
func Parse(data []byte, strict bool) (*Manifest, []Problem, error) {
	if len(data) > MaxSize {
		return nil, nil, fmt.Errorf("%s maior que %d bytes", FileName, MaxSize)
	}
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(data))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(&m); err != nil {
		var sm *toml.StrictMissingError
		if errors.As(err, &sm) {
			return nil, []Problem{{Field: "(arquivo)", Message: "campos desconhecidos:\n" + sm.String()}}, nil
		}
		return nil, nil, fmt.Errorf("ler %s: %w", FileName, err)
	}
	problems := m.sanitize()
	return &m, problems, nil
}

// cleanText normaliza espaços e limita o tamanho.
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

// relPath valida um caminho relativo dentro do repositório/pacote.
func relPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("vazio")
	}
	if len(p) > maxPath {
		return "", errors.New("longo demais")
	}
	if strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", errors.New("precisa ser relativo, com /")
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", errors.New("sai do repositório")
	}
	return c, nil
}

func isImagePath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".svg", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	}
	return false
}

// sanitize remove valores inválidos e devolve os problemas encontrados.
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
		warnf("kind", "%q: a OmaStore só indexa apps; este repositório não entra no catálogo", k)
	default:
		errf("kind", "%q desconhecido (use \"app\")", m.Kind)
		m.Kind = k
	}

	if m.Name != "" {
		n, ok := cleanText(m.Name, maxName)
		if !ok {
			warnf("name", "cortado em %d caracteres", maxName)
		}
		m.Name = n
	}
	if m.Summary != "" {
		s, ok := cleanText(m.Summary, maxSummary)
		if !ok {
			warnf("summary", "cortado em %d caracteres", maxSummary)
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
		case !mainCategories[c] && !reAdditional.MatchString(c):
			errf("categories", "categoria inválida %q", c)
			continue
		case len(cats) == maxCategories:
			warnf("categories", "só as %d primeiras são usadas", maxCategories)
			continue
		}
		seen[c] = true
		cats = append(cats, c)
	}
	m.Categories = cats
	if len(cats) > 0 && m.MainCategory() == "" {
		// Sem categoria principal o app não aparece em nenhum menu.
		warnf("categories", "nenhuma categoria principal da freedesktop (ex.: Graphics, System)")
	}

	if m.Icon != "" {
		p, err := relPath(m.Icon)
		switch {
		case err != nil:
			errf("icon", "%q: %v", m.Icon, err)
			p = ""
		case !isImagePath(p) || (path.Ext(p) != ".png" && path.Ext(p) != ".svg"):
			errf("icon", "%q: use PNG ou SVG", m.Icon)
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
			errf("screenshots", "%q não é imagem", s)
			continue
		} else {
			shots = append(shots, p)
		}
		if len(shots) == maxScreenshots {
			if len(m.Screenshots) > maxScreenshots {
				warnf("screenshots", "só as %d primeiras são usadas", maxScreenshots)
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
			errf(field, "arquitetura desconhecida (use x86_64 ou aarch64)")
			continue
		}
		if _, dup := targets[arch]; dup {
			errf(field, "arquitetura repetida")
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

// MainCategory é a primeira categoria principal declarada, ou "".
func (m *Manifest) MainCategory() string {
	for _, c := range m.Categories {
		if mainCategories[c] {
			return c
		}
	}
	return ""
}

// Target retorna o alvo da arquitetura (nome do GOARCH).
func (m *Manifest) Target(goarch string) (Target, bool) {
	if m == nil {
		return Target{}, false
	}
	t, ok := m.Linux[goarch]
	return t, ok
}

// Empty diz se o manifesto não declara nada.
func (m *Manifest) Empty() bool {
	return m == nil || ((m.Kind == "" || m.Kind == KindApp) && m.Name == "" && m.Summary == "" && len(m.Categories) == 0 && m.Icon == "" &&
		len(m.Screenshots) == 0 && m.Terminal == nil && len(m.Linux) == 0)
}

var rePlaceholder = regexp.MustCompile(`\{[^}]*\}`)

func checkPattern(p string) error {
	if len(p) > maxPath {
		return errors.New("padrão longo demais")
	}
	if strings.ContainsAny(p, "/\\") {
		return errors.New("o nome do asset não pode conter /")
	}
	if err := checkPlaceholders(p); err != nil {
		return err
	}
	if strings.Trim(p, "*") == "" {
		return errors.New("padrão genérico demais")
	}
	return nil
}

func checkPlaceholders(p string) error {
	for _, ph := range rePlaceholder.FindAllString(p, -1) {
		if ph != "{version}" && ph != "{tag}" {
			return fmt.Errorf("marcador desconhecido %s (use {version} ou {tag})", ph)
		}
	}
	return nil
}

// Expand troca {version} (tag sem "v") e {tag} pelo valor da release.
func Expand(pattern, tag string) string {
	version := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	return strings.NewReplacer("{version}", version, "{tag}", tag).Replace(pattern)
}

// MatchAsset diz se o nome do asset casa com o padrão para a tag dada.
func MatchAsset(pattern, tag, name string) bool {
	return globMatch(Expand(pattern, tag), name)
}

// globMatch casa * com qualquer sequência (sem outros metacaracteres).
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

// Encode serializa o manifesto (já validado) para guardar no banco. Um
// manifesto vazio vira "{...kind...}", nunca "": a presença do arquivo é o
// que habilita a indexação.
func (m *Manifest) Encode() string {
	if m == nil {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// Decode lê o que Encode gravou. Um valor vazio ou inválido vira nil.
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
