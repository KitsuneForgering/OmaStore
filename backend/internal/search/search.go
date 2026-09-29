package search

import (
	"math"
	"sort"
	"strings"
)

// Doc is an app indexed for search.
type Doc struct {
	Repo     string // owner/repo (identifier and final tiebreaker)
	Name     string
	Summary  string
	Readme   string
	Topics   []string
	Category string
	Stars    int
}

// Field weights: a term in the name is worth more than one in the README.
const (
	weightName    = 4.0
	weightRepo    = 2.0 // repository owner and name
	weightTopics  = 2.5
	weightSummary = 1.5
	weightReadme  = 0.6
	// Only the beginning of the README counts: that is where the description
	// is; the rest is usually installation, changelog and license.
	maxReadme = 4000

	bm25K1 = 1.2
	bm25B  = 0.75

	// Weight of the README in the similarity vectors (see indexed.sim).
	simReadmeWeight = 0.15

	// Weights of approximate matches.
	prefixWeight = 0.75
	fuzzyWeight  = 0.5
)

// indexed keeps a document's weighted terms.
type indexed struct {
	doc   Doc
	terms map[string]float64 // term → frequency weighted by the fields
	len   float64
	// sim are the terms used for similarity: the README weighs much less,
	// because boilerplate sections (installation, shortcuts, license) bring
	// unrelated apps closer together.
	sim map[string]float64
}

// Index is an immutable index; create another one when the catalog changes.
type Index struct {
	docs   []indexed
	df     map[string]int // how many documents each term appears in
	avgLen float64
	vocab  []string // sorted terms (for deterministic prefix search)
	vecs   []map[string]float64
}

// Build builds the index. The order of docs does not affect the results.
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

// Len is the number of documents.
func (ix *Index) Len() int { return len(ix.docs) }

func (ix *Index) idf(t string) float64 {
	n := float64(len(ix.docs))
	df := float64(ix.df[t])
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// expansion is a vocabulary term that matches a query term.
type expansion struct {
	weight float64 // 1 exact, prefixWeight or fuzzyWeight
	idf    float64
}

// expand returns the vocabulary terms that match a query term: the term
// itself (weight 1), terms that start with it (queries with 3+ letters,
// weight 0.75) and, only if the term is not in the vocabulary, terms one
// typo away (5+ letters, weight 0.5). The variants' idf is capped at the
// exact term's: a rare variant is never worth more than the term the user
// typed.
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

// Result is a found document.
type Result struct {
	Repo  string
	Score float64
}

// Search sorts the documents by relevance to the query. Every term must
// match (logical AND); if no document satisfies that, the search is
// repeated with OR. Ties: more stars, then the repo name.
func (ix *Index) Search(query string, limit int) []Result {
	qterms := uniq(Tokens(query))
	if len(qterms) == 0 || len(ix.docs) == 0 {
		return nil
	}
	expansions := make([]map[string]expansion, len(qterms))
	for i, q := range qterms {
		expansions[i] = ix.expand(q)
		// Portuguese term: its translation counts as the same query term.
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
		// Popularity only as a light tiebreaker between similar relevances.
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

// round removes floating point noise so the ordering does not depend on
// the order of the sums.
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
