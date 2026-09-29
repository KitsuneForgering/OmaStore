package gitrepo

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Estas funções trabalham sobre listas de caminhos relativos ("/"), vindas de
// um clone (ListFiles) ou da Trees API do GitHub.

var imageExt = map[string]bool{".png": true, ".svg": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true}

// IsImage diz se p tem extensão de imagem.
func IsImage(p string) bool { return imageExt[strings.ToLower(path.Ext(p))] }

var shotWords = []string{"screenshot", "screen-shot", "screen_shot", "screencap", "preview", "showcase", "demo"}

func isScreenshotPath(lp string) bool {
	for _, w := range shotWords {
		if strings.Contains(lp, w) {
			return true
		}
	}
	return false
}

var testDirs = map[string]bool{"test": true, "tests": true, "testdata": true, "fixtures": true, "__tests__": true, "examples": true}

func inTestDir(lp string) bool {
	for _, seg := range strings.Split(path.Dir(lp), "/") {
		if testDirs[seg] {
			return true
		}
	}
	return false
}

var sizeRe = regexp.MustCompile(`(?:^|[^0-9])(16|22|24|32|48|64|96|128|192|256|512|1024)(?:x(?:16|22|24|32|48|64|96|128|192|256|512|1024))?(?:[^0-9]|$)`)

// FindIcon escolhe o arquivo mais provável de ser o ícone do app, ou "".
// Aceita apenas PNG e SVG (formatos que o tema hicolor suporta).
func FindIcon(files []string, repoName string) string {
	repo := strings.ToLower(repoName)
	best, bestScore := "", 0
	for _, f := range files {
		lf := strings.ToLower(f)
		ext := path.Ext(lf)
		if ext != ".png" && ext != ".svg" {
			continue
		}
		if isScreenshotPath(lf) || inTestDir(lf) {
			continue
		}
		base := strings.TrimSuffix(path.Base(lf), ext)
		dir := path.Dir(lf)
		score := 0
		switch {
		case base == "icon" || base == "app-icon" || base == "appicon" || base == "app_icon":
			score = 100
		case base == repo || strings.TrimPrefix(base, "com.") == repo:
			score = 90
		case base == "logo" || base == repo+"-logo" || base == repo+"_logo" || base == repo+"-icon":
			score = 80
		case strings.Contains(base, "icon"):
			score = 50
		case strings.Contains(base, "logo"):
			score = 40
		case strings.Contains(dir, "icons/hicolor") && strings.Contains(base, repo):
			score = 70
		default:
			continue
		}
		if strings.Contains(dir, "icon") || strings.Contains(dir, "assets") || strings.Contains(dir, "resources") {
			score += 5
		}
		if strings.Contains(dir, "favicon") || strings.Contains(base, "favicon") {
			score -= 30
		}
		if dir == "." {
			score += 3
		}
		// SVG escala sem perda; entre PNGs, preferir resoluções maiores (até 512).
		if ext == ".svg" {
			score += 10
		} else if m := sizeRe.FindStringSubmatch(lf); m != nil {
			n, _ := strconv.Atoi(m[1])
			switch {
			case n >= 256 && n <= 512:
				score += 8
			case n >= 128:
				score += 5
			case n < 64:
				score -= 10
			}
		}
		// Menos profundo ganha no empate.
		score -= strings.Count(f, "/")
		if score > bestScore || (score == bestScore && f < best) {
			best, bestScore = f, score
		}
	}
	return best
}

// FindScreenshots lista imagens que parecem screenshots, em ordem estável.
func FindScreenshots(files []string, max int) []string {
	var out []string
	for _, f := range files {
		if !IsImage(f) {
			continue
		}
		if isScreenshotPath(strings.ToLower(f)) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}
