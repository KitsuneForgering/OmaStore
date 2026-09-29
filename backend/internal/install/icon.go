package install

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // JPEG icons coming from the README
	"image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// iconSizes are the hicolor theme sizes.
var iconSizes = []int{16, 22, 24, 32, 48, 64, 96, 128, 192, 256, 512}

// looksLikeSVG checks, without interpreting the file, that it is an SVG.
func looksLikeSVG(b []byte) bool {
	head := b
	if len(head) > 1024 {
		head = head[:1024]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<svg") || (strings.HasPrefix(strings.TrimSpace(s), "<?xml") && bytes.Contains(bytes.ToLower(b), []byte("<svg")))
}

// iconSize picks the largest hicolor size that does not require upscaling.
func iconSize(w, h int) int {
	m := max(w, h)
	best := iconSizes[0]
	for _, s := range iconSizes {
		if s <= m {
			best = s
		}
	}
	return best
}

// prepareIcon validates an icon's bytes and returns the final content, the
// extension and the hicolor subdirectory ("scalable" or "NxN"). Raster
// images are normalized to a square PNG at a standard size.
func prepareIcon(data []byte) (out []byte, ext, sizeDir string, err error) {
	if looksLikeSVG(data) {
		return data, ".svg", "scalable", nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid icon: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 16 || h < 16 {
		return nil, "", "", fmt.Errorf("icon too small (%dx%d)", w, h)
	}
	size := iconSize(w, h)
	if w == size && h == size && bytes.HasPrefix(data, []byte("\x89PNG")) {
		return data, ".png", fmt.Sprintf("%dx%d", size, size), nil
	}
	// Scale keeping the aspect ratio and center it on a transparent square canvas.
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	sw, sh := size, size
	if w > h {
		sh = h * size / w
	} else if h > w {
		sw = w * size / h
	}
	off := image.Pt((size-sw)/2, (size-sh)/2)
	xdraw.CatmullRom.Scale(dst, image.Rectangle{Min: off, Max: off.Add(image.Pt(sw, sh))}, img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, "", "", err
	}
	return buf.Bytes(), ".png", fmt.Sprintf("%dx%d", size, size), nil
}

// writeIcon writes the icon to the user's hicolor theme and returns its path.
func (in *Installer) writeIcon(t *tx, name string, data []byte) (string, error) {
	out, ext, sizeDir, err := prepareIcon(data)
	if err != nil {
		return "", err
	}
	p := filepath.Join(in.Paths.Icons, sizeDir, "apps", name+ext)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := t.prepare(p); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return "", err
	}
	return p, nil
}
