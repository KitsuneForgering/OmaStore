// Package search implementa a busca e as recomendações ("apps parecidos")
// do catálogo. Tudo é local e determinístico: a mesma entrada produz sempre
// o mesmo resultado, com empates resolvidos pelo nome do repositório.
package search

import (
	"strings"
	"unicode"
)

// fold troca letras acentuadas pela versão sem acento.
var fold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y", "ÿ", "y", "ß", "ss",
)

// stopwords em inglês e português que não ajudam a distinguir apps.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		a an and are as at be but by for from has have in into is it its of on or that the
		this to was were will with your you can not all any via using use based built
		o os as um uma uns umas e de do da dos das em no na nos nas por para com sem que
		se ao aos seu sua seus suas mais muito como ser esta este isso
		they them their there these those then than what when where which who how here
		also just only other some such very more most much many each every into over
		get got make made need needs out up so if do does did been being about
		app apps application tool`) {
		stopwords[w] = true
	}
}

// Tokens divide o texto em termos normalizados: minúsculas, sem acentos,
// só letras e dígitos, sem stopwords, com plural simples removido.
func Tokens(text string) []string {
	text = fold.Replace(strings.ToLower(text))
	var out []string
	for _, f := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(f) < 2 || stopwords[f] {
			continue
		}
		out = append(out, stem(f))
	}
	return out
}

// stem remove plurais regulares (inglês e português) e os sufixos
// ingleses -ing/-ed de palavras longas. É propositalmente simples:
// previsível importa mais que precisão.
func stem(w string) string {
	if len(w) <= 4 || !isAlpha(w) {
		return w
	}
	if s, ok := stripIngEd(w); ok {
		return s
	}
	switch {
	case strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "oes"), strings.HasSuffix(w, "aes"):
		return w[:len(w)-3] + "ao" // edições → edicao (após fold: edicoes)
	case strings.HasSuffix(w, "ss"), strings.HasSuffix(w, "us"), strings.HasSuffix(w, "is"):
		return w
	case strings.HasSuffix(w, "s"):
		return w[:len(w)-1]
	}
	return w
}

// stripIngEd aplica o passo 1b do stemmer de Porter: "theming" → "theme",
// "recording" → "record", "running" → "run", "annotated" → "annotate".
func stripIngEd(w string) (string, bool) {
	var base string
	switch {
	case strings.HasSuffix(w, "ing") && len(w) >= 6:
		base = w[:len(w)-3]
	case strings.HasSuffix(w, "ed") && len(w) >= 5 && !strings.HasSuffix(w, "eed"):
		base = w[:len(w)-2]
	default:
		return w, false
	}
	if !strings.ContainsAny(base, "aeiouy") {
		return w, false // "string", "bed": não é sufixo
	}
	switch {
	case strings.HasSuffix(base, "at"), strings.HasSuffix(base, "bl"), strings.HasSuffix(base, "iz"):
		return base + "e", true
	case len(base) >= 2 && base[len(base)-1] == base[len(base)-2] && !strings.ContainsRune("lsz", rune(base[len(base)-1])):
		return base[:len(base)-1], true // running → run
	case measure(base) == 1 && endsCVC(base):
		return base + "e", true // theming → theme
	}
	return base, true
}

func isVowel(w string, i int) bool {
	switch w[i] {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	case 'y':
		return i > 0 && !isVowel(w, i-1)
	}
	return false
}

// measure conta as sequências vogal-consoante (o "m" de Porter).
func measure(w string) int {
	m := 0
	prevVowel := false
	for i := range w {
		v := isVowel(w, i)
		if prevVowel && !v {
			m++
		}
		prevVowel = v
	}
	return m
}

// endsCVC: termina em consoante-vogal-consoante, e a última não é w, x ou y.
func endsCVC(w string) bool {
	n := len(w)
	if n < 3 {
		return false
	}
	last := w[n-1]
	return !isVowel(w, n-3) && isVowel(w, n-2) && !isVowel(w, n-1) && last != 'w' && last != 'x' && last != 'y'
}

func isAlpha(w string) bool {
	for _, r := range w {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// editDistance1 diz se a e b diferem por no máximo uma inserção, remoção,
// substituição ou transposição de letras vizinhas.
func editDistance1(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := len(a), len(b)
	if la-lb > 1 || lb-la > 1 {
		return false
	}
	if la == lb {
		diff := -1
		for i := 0; i < la; i++ {
			if a[i] != b[i] {
				if diff >= 0 {
					// Segunda diferença: só vale se for transposição vizinha.
					return diff == i-1 && a[diff] == b[i] && a[i] == b[diff] && a[i+1:] == b[i+1:]
				}
				diff = i
			}
		}
		return true
	}
	if la < lb {
		a, b = b, a
	}
	// a é um caractere maior que b.
	for i := 0; i < len(b); i++ {
		if a[i] != b[i] {
			return a[i+1:] == b[i:]
		}
	}
	return true
}
