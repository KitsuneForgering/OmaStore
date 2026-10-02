package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/elfdeps"
	"github.com/KitsuneForgering/OmaStore/backend/internal/sysdeps"
)

// Starred reports whether the GitHub user (the token's owner) starred the
// app's repository.
func (a *App) Starred(ctx context.Context, fullName string) (bool, error) {
	d, err := a.Store.GetApp(ctx, fullName)
	if err != nil {
		return false, err
	}
	return a.GitHub.IsStarred(ctx, d.FullName)
}

// Star stars (starred = true) or unstars the app's repository on GitHub and
// returns the new star count shown in the catalog.
func (a *App) Star(ctx context.Context, fullName string, starred bool) (int, error) {
	d, err := a.Store.GetApp(ctx, fullName)
	if err != nil {
		return 0, err
	}
	was, err := a.GitHub.IsStarred(ctx, d.FullName)
	if err != nil {
		return 0, err
	}
	if was == starred {
		return d.Repo.Stars, nil
	}
	if err := a.GitHub.SetStarred(ctx, d.FullName, starred); err != nil {
		return 0, err
	}
	delta := 1
	if !starred {
		delta = -1
	}
	return a.Store.AddStars(ctx, d.FullName, delta, time.Now())
}

// declaredDeps returns the system dependencies stored for the app.
func (a *App) declaredDeps(ctx context.Context, fullName string) (sysdeps.Set, error) {
	d, err := a.Store.GetApp(ctx, fullName)
	if err != nil {
		return sysdeps.Set{}, err
	}
	var set sysdeps.Set
	if d.SysDeps != "" {
		if err := json.Unmarshal([]byte(d.SysDeps), &set); err != nil {
			return sysdeps.Set{}, fmt.Errorf("system dependencies of %s: %w", d.FullName, err)
		}
	}
	return set, nil
}

// SysDeps checks the app's declared system dependencies on this machine
// and, once it is installed, the shared libraries its executable needs.
func (a *App) SysDeps(ctx context.Context, fullName string) (sysdeps.Report, error) {
	set, err := a.declaredDeps(ctx, fullName)
	if err != nil {
		return sysdeps.Report{}, err
	}
	rep, err := a.Pacman.Check(ctx, set)
	if err != nil && !errors.Is(err, sysdeps.ErrNoPacman) {
		return rep, err
	}
	a.addLibraries(ctx, fullName, &rep)
	return rep, err
}

// addLibraries reads the installed executable's ELF headers (never runs it)
// and adds the libraries this system lacks, with the package that ships each
// one when pacman's file database can tell. Most release tarballs have no
// PKGBUILD, so this is often the only warning before "it does not open".
func (a *App) addLibraries(ctx context.Context, fullName string, rep *sysdeps.Report) {
	inst, err := a.Store.GetInstall(ctx, fullName)
	if err != nil || inst.ExecPath == "" {
		return
	}
	root := versionRoot(a.Paths.AppsDir, inst.ExecPath)
	if root == "" {
		return
	}
	res, err := elfdeps.Check(inst.ExecPath, root, elfdeps.SystemDirs())
	if err != nil {
		if !errors.Is(err, elfdeps.ErrNotELF) {
			a.Log.Debug("library check failed", "repo", fullName, "err", err)
		}
		return
	}
	if res.WrongArch {
		rep.WrongArch = res.Machine
		return
	}
	for _, lib := range res.Missing {
		st := sysdeps.LibState{Name: lib, Status: sysdeps.StatusUnknown}
		pkg, known, err := a.Pacman.LibraryPackage(ctx, lib)
		switch {
		case err != nil:
			a.Log.Debug("library package lookup failed", "lib", lib, "err", err)
		case pkg != "":
			st.Status, st.Package = sysdeps.StatusAvailable, pkg
		case known:
			st.Status = sysdeps.StatusUnavailable
		}
		rep.Libraries = append(rep.Libraries, st)
	}
}

// versionRoot is the version directory (apps/<owner>__<repo>/<version>)
// holding exec, or "" if exec is not under appsDir.
func versionRoot(appsDir, exec string) string {
	rel, err := filepath.Rel(appsDir, exec)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}
	return filepath.Join(appsDir, parts[0], parts[1])
}

// InstallSysDeps installs, as root through polkit, every missing dependency
// (depends and optdepends) that a pacman repository has. Dependencies that
// only exist elsewhere (AUR) are left out and show up in the returned report;
// if nothing else was missing, the result is sysdeps.ErrUnavailable.
func (a *App) InstallSysDeps(ctx context.Context, fullName string) (sysdeps.Report, error) {
	rep, err := a.SysDeps(ctx, fullName)
	if err != nil {
		return rep, err
	}
	pkgs := rep.ToInstall()
	if len(pkgs) == 0 {
		for _, d := range rep.Deps {
			if d.Status == sysdeps.StatusUnavailable {
				return rep, sysdeps.ErrUnavailable
			}
		}
		return rep, nil
	}
	a.Log.Info("installing system dependencies", "repo", fullName, "packages", pkgs)
	if err := a.Pacman.Install(ctx, pkgs); err != nil {
		return rep, err
	}
	return a.SysDeps(ctx, fullName)
}
