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

// Extraction limits, against decompression bombs.
var (
	maxExtractBytes int64 = 8 << 30
	maxExtractFiles       = 100_000
)

// ErrUnsafePath means an archive entry that would leave the destination.
var ErrUnsafePath = errors.New("unsafe path in archive")

// extractor writes entries inside dest, making sure nothing leaves it.
type extractor struct {
	dest  string
	bytes int64
	files int
	// filter, if set, decides whether an entry is extracted and under which name.
	filter func(name string) (string, bool)
}

// safeJoin resolves name inside dest and rejects absolute paths or paths
// that escape with "..". It also refuses to traverse already-extracted symlinks.
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
	// No parent directory (inside dest) may be a symlink: otherwise an
	// entry "link/file" would write through it.
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

// linkInside reports whether a symlink at linkPath pointing to target stays
// inside dest. Absolute targets are never accepted.
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
		return fmt.Errorf("archive with too many entries (> %d)", maxExtractFiles)
	}
	if x.bytes > maxExtractBytes {
		return fmt.Errorf("extracted content too large (> %d bytes)", maxExtractBytes)
	}
	return nil
}

// writeFile copies r to p with the given mode, without setuid/setgid/sticky bits.
func (x *extractor) writeFile(p string, r io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	perm := mode.Perm() & 0o755
	if perm == 0 {
		perm = 0o644
	}
	// O_EXCL: a duplicate entry or a pre-existing symlink is not followed.
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
		return fmt.Errorf("extracted content too large (> %d bytes)", maxExtractBytes)
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

// extractTar extracts a (already decompressed) tar.
func (x *extractor) extractTar(r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
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
				return fmt.Errorf("extract %s: %w", h.Name, err)
			}
		case tar.TypeSymlink:
			if !x.linkInside(p, h.Linkname) {
				return fmt.Errorf("%w: symlink %q → %q leaves the destination", ErrUnsafePath, h.Name, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			os.Remove(p)
			if err := os.Symlink(h.Linkname, p); err != nil {
				return err
			}
		case tar.TypeLink:
			// Hardlink: copy the target's content (already extracted and inside dest).
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
				return fmt.Errorf("%w: hardlink %q to an invalid target", ErrUnsafePath, h.Name)
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
			// Devices, FIFOs etc. are ignored.
		}
	}
}

// extractZip extracts a zip.
func (x *extractor) extractZip(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
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
				return fmt.Errorf("%w: symlink %q → %q leaves the destination", ErrUnsafePath, f.Name, target)
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
				return fmt.Errorf("extract %s: %w", f.Name, err)
			}
		}
	}
	return nil
}

// pkgFilter accepts only the usr/ content of an Arch package. Metadata and
// install scripts (.PKGINFO, .INSTALL, .MTREE...) are discarded: nothing
// from the package is ever run.
func pkgFilter(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	if !strings.HasPrefix(name, "usr/") {
		return "", false
	}
	return name, true
}

// Extract extracts archive (in the given format) into dest. For FormatBinary and
// FormatAppImage, it copies the file as an executable named binName.
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
			return fmt.Errorf("%w: binary name %q", ErrUnsafePath, binName)
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
			return fmt.Errorf("open gzip: %w", err)
		}
		defer gz.Close()
		r = gz
	case index.FormatTarXz:
		xr, err := xz.NewReader(f)
		if err != nil {
			return fmt.Errorf("open xz: %w", err)
		}
		r = xr
	case index.FormatTarBz2:
		r = bzip2.NewReader(f)
	case index.FormatTarZst, index.FormatPkg:
		zr, err := zstd.NewReader(f)
		if err != nil {
			return fmt.Errorf("open zstd: %w", err)
		}
		defer zr.Close()
		r = zr
		if format == index.FormatPkg {
			x.filter = pkgFilter
		}
	default:
		return fmt.Errorf("unsupported format: %q", format)
	}
	return x.extractTar(r)
}
