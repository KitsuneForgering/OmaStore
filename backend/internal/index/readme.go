package index

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// repoURLs builds absolute URLs for a repository's files at a commit.
type repoURLs struct {
	FullName string
	Ref      string // commit SHA (or branch)
	BaseDir  string // README directory inside the repo ("" = root)
}

// Raw returns the raw content URL of p (relative to the repo root).
func (u repoURLs) Raw(p string) string {
	return "https://raw.githubusercontent.com/" + u.FullName + "/" + u.Ref + "/" + escapePath(p)
}

// Blob returns the GitHub page URL of file p.
func (u repoURLs) Blob(p string) string {
	return "https://github.com/" + u.FullName + "/blob/" + u.Ref + "/" + escapePath(p)
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// resolve converts a README reference into an absolute URL. Absolute
// references (http, https, mailto, anchors) are kept; data: and javascript:
// are dropped.
func (u repoURLs) resolve(ref string, image bool) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return ref
	}
	if pu, err := url.Parse(ref); err == nil && pu.Scheme != "" {
		switch strings.ToLower(pu.Scheme) {
		case "http", "https", "mailto":
			return rawifyGitHub(ref, image)
		default:
			return ""
		}
	}
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	rel, frag, _ := strings.Cut(ref, "#")
	rel, _, _ = strings.Cut(rel, "?")
	var p string
	if strings.HasPrefix(rel, "/") {
		p = path.Clean(strings.TrimPrefix(rel, "/"))
	} else {
		p = path.Clean(path.Join(u.BaseDir, rel))
	}
	if p == "." || strings.HasPrefix(p, "..") {
		return ""
	}
	if unescaped, err := url.PathUnescape(p); err == nil {
		p = unescaped
	}
	if image {
		return u.Raw(p)
	}
	out := u.Blob(p)
	if frag != "" {
		out += "#" + frag
	}
	return out
}

var reBlobURL = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/blob/(.+)$`)

// rawifyGitHub replaces github.com/.../blob/... image links with the raw URL,
// which is the one that actually serves the image.
func rawifyGitHub(ref string, image bool) string {
	if !image {
		return ref
	}
	if m := reBlobURL.FindStringSubmatch(ref); m != nil {
		return "https://raw.githubusercontent.com/" + m[1] + "/" + strings.TrimSuffix(m[2], "?raw=true")
	}
	return ref
}

var (
	reMdImage   = regexp.MustCompile(`!\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(\s+"[^"]*")?\s*\)`)
	reMdLink    = regexp.MustCompile(`(^|[^!])\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(\s+"[^"]*")?\s*\)`)
	reHTMLImg   = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc\s*=\s*)(["'])([^"']+)(["'])`)
	reHTMLHref  = regexp.MustCompile(`(?i)(<a\b[^>]*?\bhref\s*=\s*)(["'])([^"']+)(["'])`)
	reRefDef    = regexp.MustCompile(`(?m)^(\s{0,3}\[[^\]]+\]:\s*)(\S+)`)
	reComment   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reCodeFence = regexp.MustCompile("(?ms)^\\s*(```|~~~).*?^\\s*(```|~~~)\\s*$")
)

// RewriteReadme makes the README's relative image and link URLs absolute,
// so it can be displayed outside GitHub.
func RewriteReadme(md string, u repoURLs) string {
	md = reComment.ReplaceAllString(md, "")
	md = reMdImage.ReplaceAllStringFunc(md, func(s string) string {
		m := reMdImage.FindStringSubmatch(s)
		return "![" + m[1] + "](" + u.resolve(m[2], true) + m[3] + ")"
	})
	md = reMdLink.ReplaceAllStringFunc(md, func(s string) string {
		m := reMdLink.FindStringSubmatch(s)
		return m[1] + "[" + m[2] + "](" + u.resolve(m[3], false) + m[4] + ")"
	})
	md = reHTMLImg.ReplaceAllStringFunc(md, func(s string) string {
		m := reHTMLImg.FindStringSubmatch(s)
		return m[1] + m[2] + u.resolve(m[3], true) + m[4]
	})
	md = reHTMLHref.ReplaceAllStringFunc(md, func(s string) string {
		m := reHTMLHref.FindStringSubmatch(s)
		return m[1] + m[2] + u.resolve(m[3], false) + m[4]
	})
	md = reRefDef.ReplaceAllStringFunc(md, func(s string) string {
		m := reRefDef.FindStringSubmatch(s)
		return m[1] + u.resolve(m[2], IsImageURL(m[2]))
	})
	return md
}

// IsImageURL reports whether the URL points to an image, by extension.
func IsImageURL(s string) bool {
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")
	switch strings.ToLower(path.Ext(s)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return true
	}
	return false
}

var badgeHosts = []string{
	"shields.io", "badgen.net", "badge.fury.io", "travis-ci", "circleci.com", "codecov.io",
	"coveralls.io", "goreportcard.com", "pkg.go.dev/badge", "godoc.org", "api.star-history.com",
	"ko-fi.com", "buymeacoffee.com", "liberapay.com", "img.youtube.com", "deepwiki.com",
	"forthebadge.com", "repology.org", "awesome.re", "contrib.rocks",
}

// isBadge detects badges and other decorative images that are not screenshots.
func isBadge(u string) bool {
	l := strings.ToLower(u)
	for _, h := range badgeHosts {
		if strings.Contains(l, h) {
			return true
		}
	}
	return strings.Contains(l, "/badge") || strings.Contains(l, "badge.svg") ||
		strings.Contains(l, "/workflows/") || strings.HasSuffix(l, ".svg")
}

// ReadmeImages lists the README images (already absolute) that may be
// screenshots, in the order they appear and without duplicates.
func ReadmeImages(rewritten string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(u string) {
		if u == "" || seen[u] || isBadge(u) || !strings.HasPrefix(u, "https://") {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	md := reCodeFence.ReplaceAllString(rewritten, "")
	type hit struct {
		pos int
		url string
	}
	var hits []hit
	for _, m := range reMdImage.FindAllStringSubmatchIndex(md, -1) {
		hits = append(hits, hit{m[0], md[m[4]:m[5]]})
	}
	for _, m := range reHTMLImg.FindAllStringSubmatchIndex(md, -1) {
		hits = append(hits, hit{m[0], md[m[6]:m[7]]})
	}
	// Sort by position in the text.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].pos < hits[j-1].pos; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	for _, h := range hits {
		add(h.url)
	}
	return out
}

var (
	reHeading  = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	reSetext   = regexp.MustCompile(`^\s{0,3}(=+|-+)\s*$`)
	reHTMLTag  = regexp.MustCompile(`<[^>]+>`)
	reInlineMd = regexp.MustCompile("[*_`~]+")
	reSpaces   = regexp.MustCompile(`\s+`)
)

// Title returns the text of the README's first heading (H1/H2), or "".
func Title(md string) string {
	md = reCodeFence.ReplaceAllString(reComment.ReplaceAllString(md, ""), "")
	for _, line := range strings.Split(md, "\n") {
		if m := reHeading.FindStringSubmatch(line); m != nil {
			return plain(m[1])
		}
		if h := reHTMLHeading.FindStringSubmatch(line); h != nil {
			if t := plain(h[1]); t != "" {
				return t
			}
		}
	}
	return ""
}

var reHTMLHeading = regexp.MustCompile(`(?i)<h[12][^>]*>(.*?)</h[12]>`)

// plain removes markdown/HTML markup from a line.
func plain(s string) string {
	s = reMdImage.ReplaceAllString(s, "")
	s = reMdLink.ReplaceAllString(s, "$1$2")
	s = reHTMLTag.ReplaceAllString(s, "")
	s = reInlineMd.ReplaceAllString(s, "")
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ").Replace(s)
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// Summary extracts the README's first text paragraph, skipping headings,
// badges, images, lists, tables, quotes and code blocks.
func Summary(md string, max int) string {
	md = reCodeFence.ReplaceAllString(reComment.ReplaceAllString(md, ""), "")
	var para []string
	flush := func() string {
		s := plain(strings.Join(para, " "))
		para = para[:0]
		return s
	}
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		skip := t == "" || reHeading.MatchString(line) || reSetext.MatchString(line) ||
			strings.HasPrefix(t, "|") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "- ") ||
			strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") || strings.HasPrefix(t, "    ") ||
			strings.HasPrefix(t, "<h") || strings.HasPrefix(t, "<H")
		if skip {
			if s := flush(); len(s) >= 20 {
				return truncate(s, max)
			}
			continue
		}
		para = append(para, t)
	}
	if s := flush(); len(s) >= 20 {
		return truncate(s, max)
	}
	return ""
}

func truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	cut := string(r[:max])
	// Cut at the last whole word, unless the cut already falls on a space.
	if i := strings.LastIndexAny(cut, " \t"); r[max] != ' ' && i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}
