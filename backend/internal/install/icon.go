package install

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // ícones em JPEG vindos do README
	"image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// iconSizes são os tamanhos do tema hicolor.
var iconSizes = []int{16, 22, 24, 32, 48, 64, 96, 128, 192, 256, 512}

// looksLikeSVG confere, sem interpretar o arquivo, que ele é um SVG.
func looksLikeSVG(b []byte) bool {
	head := b
	if len(head) > 1024 {
		head = head[:1024]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<svg") || (strings.HasPrefix(strings.TrimSpace(s), "<?xml") && bytes.Contains(bytes.ToLower(b), []byte("<svg")))
}

// iconSize escolhe o maior tamanho hicolor que não exige ampliar a imagem.
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

// prepareIcon valida os bytes de um ícone e devolve o conteúdo final, a
// extensão e o subdiretório hicolor ("scalable" ou "NxN"). Imagens raster
// são normalizadas para PNG quadrado num tamanho padrão.
func prepareIcon(data []byte) (out []byte, ext, sizeDir string, err error) {
	if looksLikeSVG(data) {
		return data, ".svg", "scalable", nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", "", fmt.Errorf("ícone inválido: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 16 || h < 16 {
		return nil, "", "", fmt.Errorf("ícone pequeno demais (%dx%d)", w, h)
	}
	size := iconSize(w, h)
	if w == size && h == size && bytes.HasPrefix(data, []byte("\x89PNG")) {
		return data, ".png", fmt.Sprintf("%dx%d", size, size), nil
	}
	// Escala mantendo a proporção e centraliza numa tela quadrada transparente.
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

// writeIcon grava o ícone no tema hicolor do usuário e retorna o caminho.
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
