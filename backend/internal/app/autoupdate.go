package app

import (
	"context"
	"runtime"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// AutoUpdate reports whether updates install on their own. On by default:
// the user turns it off in the interface or with `omastore auto-update off`.
func (a *App) AutoUpdate(ctx context.Context) (bool, error) {
	v, err := a.Store.Setting(ctx, store.SettingAutoUpdate, "on")
	return v != "off", err
}

// SetAutoUpdate turns automatic updates on or off.
func (a *App) SetAutoUpdate(ctx context.Context, on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	return a.Store.SetSetting(ctx, store.SettingAutoUpdate, v)
}

// AutoUpdates lists the installed apps with a newer release, split into
// those an automatic update installs (the file can be verified) and those
// that wait for the user (nothing checks the download, or the files are gone
// and the user should see the repair).
func (a *App) AutoUpdates(ctx context.Context) (ready, waiting []store.ListItem, err error) {
	items, err := a.ListApps(ctx, store.Filter{InstalledOnly: true, All: true})
	if err != nil {
		return nil, nil, err
	}
	for _, it := range items {
		if it.LatestTag == "" || it.InstalledVersion == it.LatestTag {
			continue
		}
		d, err := a.GetApp(ctx, it.FullName)
		if err != nil {
			return nil, nil, err
		}
		sel, ok := install.SelectAsset(d.Assets, runtime.GOARCH, manifest.Decode(d.Manifest))
		switch {
		case !ok:
			// No file for this machine in the new release: nothing to do.
		case d.Install == nil || install.IsBroken(*d.Install):
			waiting = append(waiting, it)
		case install.Verifiable(sel):
			ready = append(ready, it)
		default:
			waiting = append(waiting, it)
		}
	}
	return ready, waiting, nil
}

// AutoCheckDue reports whether automatic updates are on and the installed
// apps were last refreshed for them more than every ago.
func (a *App) AutoCheckDue(ctx context.Context, every time.Duration) (bool, error) {
	on, err := a.AutoUpdate(ctx)
	if err != nil || !on {
		return false, err
	}
	v, err := a.Store.Setting(ctx, store.SettingAutoCheckAt, "")
	if err != nil {
		return false, err
	}
	last, err := time.Parse(time.RFC3339, v)
	return err != nil || time.Since(last) >= every, nil
}

// MarkAutoCheck records that the installed apps were just refreshed.
func (a *App) MarkAutoCheck(ctx context.Context) error {
	return a.Store.SetSetting(ctx, store.SettingAutoCheckAt, time.Now().UTC().Format(time.RFC3339))
}
