package search

import (
	"math"
	"sort"
)

// Termos genéricos demais no ecossistema: quase todo app do catálogo os
// tem, então não indicam semelhança.
var genericTerms = map[string]bool{
	"omarchy": true, "linux": true, "arch": true, "archlinux": true, "hyprland": true,
	"wayland": true, "desktop": true, "gui": true, "open": true, "source": true,
	"github": true, "install": true, "license": true, "mit": true, "release": true,
}

// minSimilarity descarta pares que só compartilham termos marginais.
// Medido no catálogo real: pares relacionados ficam acima de ~0,2 e o
// ruído entre apps sem relação entre 0,09 e 0,16.
const minSimilarity = 0.12

// buildVectors calcula os vetores TF-IDF normalizados usados em Similar.
func (ix *Index) buildVectors() {
	n := float64(len(ix.docs))
	ix.vecs = make([]map[string]float64, len(ix.docs))
	for i, d := range ix.docs {
		v := map[string]float64{}
		var norm float64
		for t, tf := range d.sim {
			if genericTerms[t] {
				continue
			}
			df := float64(ix.df[t])
			if df <= 1 {
				continue // termo exclusivo não aproxima de ninguém
			}
			w := math.Log1p(tf) * math.Log(n/df)
			if w <= 0 {
				continue
			}
			v[t] = w
			norm += w * w
		}
		norm = math.Sqrt(norm)
		for t := range v {
			v[t] /= norm
		}
		ix.vecs[i] = v
	}
}

// Similar retorna os apps mais parecidos com repo (sem incluí-lo), pela
// similaridade de cosseno dos vetores TF-IDF. Empates: nome do repo.
func (ix *Index) Similar(repo string, limit int) []Result {
	self := -1
	for i, d := range ix.docs {
		if d.doc.Repo == repo {
			self = i
			break
		}
	}
	if self < 0 {
		return nil
	}
	base := ix.vecs[self]
	var out []Result
	for i, v := range ix.vecs {
		if i == self {
			continue
		}
		// Percorre o menor vetor.
		a, b := base, v
		if len(a) > len(b) {
			a, b = b, a
		}
		var dot float64
		for t, w := range a {
			dot += w * b[t]
		}
		if dot = round(dot); dot >= minSimilarity {
			out = append(out, Result{ix.docs[i].doc.Repo, dot})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Repo < out[j].Repo
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
