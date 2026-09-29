package index

import (
	_ "embed"
	"strings"
)

//go:embed seeds.txt
var seedsTxt string

// Seeds retorna a lista curada de repositórios semente.
func Seeds() []string { return parseSeeds(seedsTxt) }

func parseSeeds(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line, _, _ = strings.Cut(line, "#")
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
