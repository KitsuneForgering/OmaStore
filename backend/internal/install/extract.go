package install

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
)

// Limites de extração, contra bombas de descompressão.
var (
	maxExtractBytes int64 = 8 << 30
	maxExtractFiles       = 100_000
)

// ErrUnsafePath indica uma entrada de arquivo que sairia do destino.
var ErrUnsafePath = errors.New("caminho inseguro no arquivo")

// extractor escreve entradas dentro de dest, garantindo que nada saia dele.
type extractor struct {
	dest  string
	bytes int64
	files int
	// filter, se definido, decide se uma entrada entra e com qual nome.
	filter func(name string) (string, bool)
}

// safeJoin resolve name dentro de dest e rejeita caminhos absolutos ou que
// escapem com "..". Também recusa atravessar symlinks já extraídos.
func (x *extractor) safeJoin(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	if name == "" || path.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	p := filepath.Join(x.dest, filepath.FromSlash(clean))
	// Nenhum diretório pai (dentro de dest) pode ser um symlink: senão uma
	// entrada "link/arquivo" escreveria através dele.
	rel := filepath.Dir(filepath.FromSlash(clean))
	cur := x.dest
	if rel != "." {
		for _, seg := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, seg)
			if st, err := os.Lstat(cur); err == nil && st.Mode()&fs.ModeSymlink != 0 {
				return "", fmt.Errorf("%w: %q atravessa symlink", ErrUnsafePath, name)
			}
		}
	}
	return p, nil
}

// linkInside diz se um symlink em linkPath apontando para target continua
// dentro de dest. Alvos absolutos nunca são aceitos.
func (x *extractor) linkInside(linkPath, target string) bool {
	if target == "" || filepath.IsAbs(target) || strings.HasPrefix(target, "/") {
		return false
	}
	resolved := filepath.Join(filepath.Dir(linkPath), filepath.FromSlash(target))
	rel, err := filepath.Rel(x.dest, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (x *extractor) account(n int64) error {
	x.files++
	x.bytes += n
	if x.files > maxExtractFiles {
		return fmt.Errorf("arquivo com entradas demais (> %d)", maxExtractFiles)
	}
	if x.bytes > maxExtractBytes {
		return fmt.Errorf("conteúdo extraído grande demais (> %d bytes)", maxExtractBytes)
	}
	return nil
}

// writeFile copia r para p com o modo dado, sem bits setuid/setgid/sticky.
func (x *extractor) writeFile(p string, r io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	perm := mode.Perm() & 0o755
	if perm == 0 {
		perm = 0o644
	}
	// O_EXCL: uma entrada duplicada ou um symlink pré-existente não é seguido.
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o200)
	if errors.Is(err, fs.ErrExist) {
		if rmErr := os.Remove(p); rmErr != nil {
			return rmErr
		}
		f, err = os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o200)
	}
	if err != nil {
		return err
	}
	limit := maxExtractBytes - x.bytes
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("conteúdo extraído grande demais (> %d bytes)", maxExtractBytes)
	}
	x.bytes += n
	return nil
}

func (x *extractor) name(n string) (string, bool) {
	if x.filter == nil {
		return n, true
	}
	return x.filter(n)
}

// extractTar extrai um tar (já descomprimido).
func (x *extractor) extractTar(r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("ler tar: %w", err)
		}
		name, ok := x.name(h.Name)
		if !ok {
			continue
		}
		p, err := x.safeJoin(name)
		if err != nil {
			return err
		}
		if p == "" {
			continue
		}
		if err := x.account(0); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := x.writeFile(p, tr, fs.FileMode(h.Mode)); err != nil {
				return fmt.Errorf("extrair %s: %w", h.Name, err)
			}
		case tar.TypeSymlink:
			if !x.linkInside(p, h.Linkname) {
				return fmt.Errorf("%w: symlink %q → %q sai do destino", ErrUnsafePath, h.Name, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			os.Remove(p)
			if err := os.Symlink(h.Linkname, p); err != nil {
				return err
			}
		case tar.TypeLink:
			// Hardlink: copiamos o conteúdo do alvo (já extraído e dentro de dest).
			tname, ok := x.name(h.Linkname)
			if !ok {
				continue
			}
			target, err := x.safeJoin(tname)
			if err != nil || target == "" {
				return fmt.Errorf("%w: hardlink %q → %q", ErrUnsafePath, h.Name, h.Linkname)
			}
			st, err := os.Lstat(target)
			if err != nil || !st.Mode().IsRegular() {
				return fmt.Errorf("%w: hardlink %q para alvo inválido", ErrUnsafePath, h.Name)
			}
			src, err := os.Open(target)
			if err != nil {
				return err
			}
			err = x.writeFile(p, src, st.Mode())
			src.Close()
			if err != nil {
				return err
			}
		default:
			// Dispositivos, FIFOs etc. são ignorados.
		}
	}
}

// extractZip extrai um zip.
func (x *extractor) extractZip(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("abrir zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		name, ok := x.name(f.Name)
		if !ok {
			continue
		}
		p, err := x.safeJoin(name)
		if err != nil {
			return err
		}
		if p == "" {
			continue
		}
		if err := x.account(0); err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case mode&fs.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			target := string(b)
			if !x.linkInside(p, target) {
				return fmt.Errorf("%w: symlink %q → %q sai do destino", ErrUnsafePath, f.Name, target)
			}
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.Remove(p)
			if err := os.Symlink(target, p); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = x.writeFile(p, rc, mode)
			rc.Close()
			if err != nil {
				return fmt.Errorf("extrair %s: %w", f.Name, err)
			}
		}
	}
	return nil
}

// pkgFilter aceita só o conteúdo de usr/ de um pacote do Arch. Metadados e
// scripts de instalação (.PKGINFO, .INSTALL, .MTREE...) são descartados:
// nada do pacote é executado.
func pkgFilter(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	if !strings.HasPrefix(name, "usr/") {
		return "", false
	}
	return name, true
}

// Extract extrai archive (no formato dado) para dest. Para FormatBinary e
// FormatAppImage, copia o arquivo como executável com o nome binName.
func Extract(archive, format, dest, binName string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	x := &extractor{dest: dest}
	open := func() (*os.File, error) { return os.Open(archive) }

	switch format {
	case index.FormatBinary, index.FormatAppImage:
		f, err := open()
		if err != nil {
			return err
		}
		defer f.Close()
		p, err := x.safeJoin(binName)
		if err != nil || p == "" {
			return fmt.Errorf("%w: nome de binário %q", ErrUnsafePath, binName)
		}
		return x.writeFile(p, f, 0o755)
	case index.FormatZip:
		return x.extractZip(archive)
	}

	f, err := open()
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader
	switch format {
	case index.FormatTarGz:
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("abrir gzip: %w", err)
		}
		defer gz.Close()
		r = gz
	case index.FormatTarXz:
		xr, err := xz.NewReader(f)
		if err != nil {
			return fmt.Errorf("abrir xz: %w", err)
		}
		r = xr
	case index.FormatTarBz2:
		r = bzip2.NewReader(f)
	case index.FormatTarZst, index.FormatPkg:
		zr, err := zstd.NewReader(f)
		if err != nil {
			return fmt.Errorf("abrir zstd: %w", err)
		}
		defer zr.Close()
		r = zr
		if format == index.FormatPkg {
			x.filter = pkgFilter
		}
	default:
		return fmt.Errorf("formato não suportado: %q", format)
	}
	return x.extractTar(r)
}
