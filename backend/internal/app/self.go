package app

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/github"
	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
)

// selfReleaseTTL is how long the latest OmaStore release is reused before
// asking GitHub again.
const selfReleaseTTL = 30 * time.Minute

type selfCache struct {
	mu      sync.Mutex
	release *github.Release
	at      time.Time
	exe     string // test hook: the executable to classify instead of os.Executable()
}

func (a *App) selfInstall() install.SelfInstall {
	exe := a.self.exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return install.SelfInstall{Mode: install.SelfDev, Root: a.Installer.SelfRoot()}
		}
	}
	return a.Installer.DetectSelf(exe)
}

// latestSelf returns OmaStore's latest stable release (nil if there is none).
func (a *App) latestSelf(ctx context.Context, fresh bool) (*github.Release, error) {
	a.self.mu.Lock()
	defer a.self.mu.Unlock()
	if !fresh && !a.self.at.IsZero() && time.Since(a.self.at) < selfReleaseTTL {
		return a.self.release, nil
	}
	rel, err := a.GitHub.LatestRelease(ctx, install.SelfRepo)
	if err != nil {
		return nil, err
	}
	a.self.release, a.self.at = rel, time.Now()
	return rel, nil
}

// SelfStatus compares the running OmaStore with its latest release. The
// installation mode is known even when GitHub cannot be reached.
func (a *App) SelfStatus(ctx context.Context) (install.SelfStatus, error) {
	cur := a.selfInstall()
	st := install.SelfStatus{Mode: cur.Mode, Version: cur.Version}
	if cur.Mode == install.SelfDev {
		return st, nil // a development build has no release to compare with
	}
	rel, err := a.latestSelf(ctx, false)
	if err != nil || rel == nil {
		return st, err
	}
	if v, ok := install.SelfTag(rel.Tag); ok {
		st.Latest, st.Notes = v, rel.Body
		st.UpdateAvailable = cur.Mode == install.SelfManaged && install.NewerVersion(v, cur.Version)
	}
	return st, nil
}

// SelfUpdate installs OmaStore's latest release over this installation.
// Returns install.ErrUpToDate when there is nothing newer.
func (a *App) SelfUpdate(ctx context.Context, progress func(install.Progress)) (*install.SelfResult, error) {
	cur := a.selfInstall()
	if cur.Mode != install.SelfManaged {
		return nil, install.ErrSelfNotManaged
	}
	rel, err := a.latestSelf(ctx, true)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return &install.SelfResult{From: cur.Version, To: cur.Version}, install.ErrUpToDate
	}
	version, _ := install.SelfTag(rel.Tag)
	name := a.Installer.SelfAssetName(version)
	target := install.SelfRelease{Tag: rel.Tag}
	for _, as := range rel.Assets {
		switch as.Name {
		case name:
			target.AssetURL, target.Digest = as.URL, as.Digest
		case name + ".sha256":
			target.ChecksumURL = as.URL
		}
	}
	if target.AssetURL == "" && version != "" && install.NewerVersion(version, cur.Version) {
		return nil, install.ErrSelfNoAsset
	}
	return a.Installer.SelfUpdate(ctx, cur, target, progress)
}
