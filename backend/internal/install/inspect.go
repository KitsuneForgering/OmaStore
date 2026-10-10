package install

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/elfdeps"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// Inspection describes the parts of an installation that can be checked in a
// temporary directory. No app files, launchers, or services are installed.
type Inspection struct {
	Executable        string
	Verified          bool
	Missing           []string
	WrongArch         bool
	Services          []string
	DeclaredExecError string
}

// InspectionError separates an unavailable download from a bad release file.
type InspectionError struct {
	Stage string
	Err   error
}

func (e *InspectionError) Error() string { return e.Stage + ": " + e.Err.Error() }
func (e *InspectionError) Unwrap() error { return e.Err }

// Inspect downloads and opens the same release asset that Install would use.
// It never runs code from the archive. The temporary directory is removed on
// return, including when verification or extraction fails.
func (in *Installer) Inspect(ctx context.Context, a store.Asset, repo string, m *manifest.Manifest, arch string) (Inspection, error) {
	var out Inspection
	tmp, err := os.MkdirTemp("", "omastore-check-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, "asset")
	sum256, sum512, err := in.download(ctx, a.URL, archive, nil)
	if err != nil {
		return out, &InspectionError{"download", fmt.Errorf("%s: %w", a.Name, err)}
	}
	want, err := in.expected(ctx, a.Digest, a.ChecksumURL, a.Name)
	if err != nil {
		return out, &InspectionError{"checksum", fmt.Errorf("%s: %w", a.Name, err)}
	}
	if want != "" {
		if err := verify(want, sum256, sum512); err != nil {
			return out, &InspectionError{"checksum", fmt.Errorf("%s: %w", a.Name, err)}
		}
		out.Verified = true
	}
	staging := filepath.Join(tmp, "package")
	binName := strings.ToLower(repo)
	if a.Format == asset.FormatAppImage {
		binName += ".AppImage"
	}
	if err := Extract(archive, a.Format, staging, binName); err != nil {
		return out, &InspectionError{"package", fmt.Errorf("%s: %w", a.Name, err)}
	}
	check := &Installer{GOARCH: arch, Log: in.Log}
	if target, ok := m.Target(arch); ok && target.Exec != "" && a.Format != asset.FormatBinary && a.Format != asset.FormatAppImage {
		if _, err := declaredExec(staging, manifest.Expand(target.Exec, a.Tag)); err != nil {
			out.DeclaredExecError = err.Error()
		}
	}
	exe, err := check.findExec(staging, repo, a.Format, a.Tag, m)
	if err != nil {
		return out, &InspectionError{"package", fmt.Errorf("executable in %s: %w", a.Name, err)}
	}
	rel, err := filepath.Rel(staging, exe)
	if err != nil || strings.ContainsAny(rel, "\n\r\x00") {
		return out, &InspectionError{"package", fmt.Errorf("unsafe executable path in %s", a.Name)}
	}
	out.Executable = filepath.ToSlash(rel)
	cmd := filepath.Base(exe)
	if strings.EqualFold(filepath.Ext(cmd), ".appimage") {
		cmd = strings.TrimSuffix(cmd, filepath.Ext(cmd))
	}
	if err := in.checkCommandName(strings.ToLower(cmd)); err != nil {
		return out, &InspectionError{"launcher", err}
	}
	if isELF, _ := fileKind(exe); isELF {
		f, err := elf.Open(exe)
		if err != nil {
			return out, &InspectionError{"package", fmt.Errorf("invalid ELF executable: %w", err)}
		}
		want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[arch]
		out.WrongArch = want != 0 && f.Machine != want
		f.Close()
	}
	if m != nil {
		for id, svc := range m.Services {
			if _, err := serviceExec(staging, svc.Exec); err != nil {
				return out, &InspectionError{"service", fmt.Errorf("%s: %w", id, err)}
			}
			out.Services = append(out.Services, svc.Unit)
		}
	}
	// System libraries describe only the machine running the check. Other
	// architectures still get package and executable inspection.
	if arch == in.goarch() {
		deps, err := elfdeps.Check(exe, staging, elfdeps.SystemDirs())
		if err != nil && !errors.Is(err, elfdeps.ErrNotELF) {
			return out, &InspectionError{"libraries", fmt.Errorf("%s: %w", a.Name, err)}
		}
		out.Missing = deps.Missing
		out.WrongArch = out.WrongArch || deps.WrongArch
	}
	return out, nil
}
