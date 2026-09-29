package search

import (
	"math"
	"sort"
	"strings"
)

// Doc é um app indexado para busca.
type Doc struct {
	Repo     string // owner/repo (identificador e desempate final)
	Name     string
	Summary  string
	Readme   string
	Topics   []string
	Category string
	Stars    int
}

// Pesos dos campos: um termo no nome vale mais que no README.
const (
	weightName    = 4.0
	weightRepo    = 2.0 // owner e nome do repositório
	weightTopics  = 2.5
	weightSummary = 1.5
	weightReadme  = 0.6
	// Só o começo do README entra: é onde está a descrição; o resto costuma
	// ser instalação, changelog e licença.
	maxReadme = 4000

	bm25K1 = 1.2
	bm25B  = 0.75

	// Pesos de correspondências aproximadas.
	// Peso do README nos vetores de similaridade.
	simReadmeWeight = 0.15

	prefixWeight = 0.75
	fuzzyWeight  = 0.5
)

// indexed guarda os termos ponderados de um documento.
type indexed struct {
	doc   Doc
	terms map[string]float64 // termo → frequência ponderada pelos campos
	len   float64
	// sim são os termos usados na similaridade: o README pesa bem menos,
	// porque trechos padrão (instalação, atalhos, licença) aproximam apps
	// que não têm nada a ver.
	sim map[string]float64
}

// Index é um índice imutável; crie outro quando o catálogo mudar.
type Index struct {
	docs   []indexed
	df     map[string]int // em quantos documentos cada termo aparece
	avgLen float64
	vocab  []string // termos ordenados (para busca por prefixo determinística)
	vecs   []map[string]float64
}

// Build monta o índice. A ordem de docs não afeta os resultados.
func Build(docs []Doc) *Index {
	sorted := append([]Doc(nil), docs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Repo < sorted[j].Repo })

	ix := &Index{df: map[string]int{}}
	var total float64
	for _, d := range sorted {
		in := indexed{doc: d, terms: map[string]float64{}, sim: map[string]float64{}}
		add := func(text string, w float64) {
			for _, t := range Tokens(text) {
				in.terms[t] += w
				in.len += w
				if w == weightReadme {
					in.sim[t] += simReadmeWeight
				} else if w != weightRepo {
					in.sim[t] += w
				}
			}
		}
		add(d.Name, weightName)
		add(strings.ReplaceAll(d.Repo, "/", " "), weightRepo)
		add(strings.Join(d.Topics, " "), weightTopics)
		add(d.Category, weightTopics)
		add(d.Summary, weightSummary)
		readme := d.Readme
		if len(readme) > maxReadme {
			readme = readme[:maxReadme]
		}
		add(readme, weightReadme)
		for t := range in.terms {
			ix.df[t]++
		}
		total += in.len
		ix.docs = append(ix.docs, in)
	}
	if n := len(ix.docs); n > 0 {
		ix.avgLen = total / float64(n)
	}
	for t := range ix.df {
		ix.vocab = append(ix.vocab, t)
	}
	sort.Strings(ix.vocab)
	ix.buildVectors()
	return ix
}

// Len é o número de documentos.
func (ix *Index) Len() int { return len(ix.docs) }

func (ix *Index) idf(t string) float64 {
	n := float64(len(ix.docs))
	df := float64(ix.df[t])
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// expansion é um termo do vocabulário que casa com um termo da consulta.
type expansion struct {
	weight float64 // 1 exato, prefixWeight ou fuzzyWeight
	idf    float64
}

// expand devolve os termos do vocabulário que casam com um termo da
// consulta: o próprio termo (peso 1), termos que começam com ele (consulta
// com 3+ letras, peso 0,75) e, só se o termo não existir no vocabulário,
// termos a um erro de digitação (5+ letras, peso 0,5). O idf das variantes
// é limitado ao do termo exato: uma variante rara nunca vale mais que o
// termo que o usuário digitou.
func (ix *Index) expand(q string) map[string]expansion {
	out := map[string]expansion{}
	_, exact := ix.df[q]
	capIDF := math.Inf(1)
	if exact {
		capIDF = ix.idf(q)
		out[q] = expansion{1, capIDF}
	}
	variant := func(t string, w float64) {
		out[t] = expansion{w, math.Min(ix.idf(t), capIDF)}
	}
	if len(q) >= 3 {
		i := sort.SearchStrings(ix.vocab, q)
		for ; i < len(ix.vocab) && strings.HasPrefix(ix.vocab[i], q); i++ {
			if ix.vocab[i] != q {
				variant(ix.vocab[i], prefixWeight)
			}
		}
	}
	if !exact && len(q) >= 5 {
		for _, t := range ix.vocab {
			if _, ok := out[t]; !ok && len(t) >= 4 && editDistance1(q, t) {
				variant(t, fuzzyWeight)
			}
		}
	}
	return out
}

// Result é um documento encontrado.
type Result struct {
	Repo  string
	Score float64
}

// Search ordena os documentos pela relevância para a consulta. Todos os
// termos precisam casar (E lógico); se nenhum documento satisfizer isso, a
// busca é repetida com OU. Empates: mais estrelas, depois o nome do repo.
func (ix *Index) Search(query string, limit int) []Result {
	qterms := uniq(Tokens(query))
	if len(qterms) == 0 || len(ix.docs) == 0 {
		return nil
	}
	expansions := make([]map[string]expansion, len(qterms))
	for i, q := range qterms {
		expansions[i] = ix.expand(q)
		// Termo em português: a tradução conta como o mesmo termo da consulta.
		for _, en := range ptToEn[q] {
			for t, e := range ix.expand(en) {
				if cur, ok := expansions[i][t]; !ok || e.weight*e.idf > cur.weight*cur.idf {
					expansions[i][t] = e
				}
			}
		}
	}
	res := ix.score(expansions, true)
	if len(res) == 0 && len(qterms) > 1 {
		res = ix.score(expansions, false)
	}
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	return res
}

func (ix *Index) score(expansions []map[string]expansion, all bool) []Result {
	type scored struct {
		Result
		stars int
	}
	var out []scored
	for _, d := range ix.docs {
		var total float64
		matched := 0
		for _, exp := range expansions {
			best := 0.0
			for t, e := range exp {
				tf, ok := d.terms[t]
				if !ok {
					continue
				}
				norm := tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*d.len/ix.avgLen))
				if s := e.weight * e.idf * norm; s > best {
					best = s
				}
			}
			if best > 0 {
				matched++
				total += best
			}
		}
		if matched == 0 || (all && matched < len(expansions)) {
			continue
		}
		// Popularidade só como leve desempate entre relevâncias parecidas.
		total *= 1 + 0.02*math.Log1p(float64(d.doc.Stars))
		out = append(out, scored{Result{d.doc.Repo, round(total)}, d.doc.Stars})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].stars != out[j].stars {
			return out[i].stars > out[j].stars
		}
		return out[i].Repo < out[j].Repo
	})
	res := make([]Result, len(out))
	for i, s := range out {
		res[i] = s.Result
	}
	return res
}

// round elimina ruído de ponto flutuante para que a ordenação não dependa
// da ordem das somas.
func round(f float64) float64 { return math.Round(f*1e9) / 1e9 }

func uniq(ts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range ts {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
