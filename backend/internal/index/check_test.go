package index

import (
	"context"
	"strings"
	"testing"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/manifest"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

func checkOf(r *Report, item string) (Check, bool) {
	for _, c := range r.Checks {
		if strings.HasPrefix(c.Item, item) {
			return c, true
		}
	}
	return Check{}, false
}

func TestCheckCompatibleRepoWritesNothing(t *testing.T) {
	ix, _, st := setup(t)
	r, err := ix.Check(context.Background(), "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Compatible() {
		t.Errorf("checks = %+v", r.Checks)
	}
	if c, _ := checkOf(r, "Installable on x86_64"); c.Status != CheckOK {
		t.Errorf("installable = %+v", c)
	}
	if c, _ := checkOf(r, "Checksum"); c.Status != CheckOK {
		t.Errorf("checksum = %+v", c)
	}
	if r.Name != "OmaPhoto" || r.Tag != "v1.0.0" || len(r.Screenshots) == 0 {
		t.Errorf("report = %+v", r)
	}
	if names, _ := st.RepoNames(context.Background()); len(names) != 0 {
		t.Errorf("Check wrote to the database: %v", names)
	}
}

func TestCheckWithoutManifestSuggestsOne(t *testing.T) {
	ix, gh, _ := setup(t)
	gh.repos["acme/omaphoto"].noToml = true
	r, err := ix.Check(context.Background(), "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Compatible() {
		t.Error("a repository without omastore.toml must not be compatible")
	}
	if c, _ := checkOf(r, "omastore.toml"); c.Status != CheckFail {
		t.Errorf("manifest = %+v", c)
	}
	s := r.SuggestedManifest
	for _, want := range []string{`name = "OmaPhoto"`, `icon = "assets/icon.svg"`, "[linux.x86_64]",
		`asset = "omaphoto-{version}-x86_64-linux.tar.gz"`, "[linux.aarch64]"} {
		if !strings.Contains(s, want) {
			t.Errorf("suggestion lacks %q:\n%s", want, s)
		}
	}
	m, problems, err := manifest.Parse([]byte(s), true)
	if err != nil || len(problems) > 0 || m == nil {
		t.Fatalf("suggestion does not lint: %v %v\n%s", err, problems, s)
	}
	if tg, _ := m.Target(ArchAMD64); !manifest.MatchAsset(tg.Asset, "v1.0.0", "omaphoto-1.0.0-x86_64-linux.tar.gz") {
		t.Errorf("pattern %q does not match the release", tg.Asset)
	}
}

func TestCheckOverrideAndFailures(t *testing.T) {
	ix, gh, _ := setup(t)
	ctx := context.Background()

	theme := `kind = "theme"`
	r, err := ix.Check(ctx, "acme/omaphoto", &theme)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := checkOf(r, "Kind"); c.Status != CheckFail {
		t.Errorf("theme: %+v", r.Checks)
	}

	unknown := "icone = \"x.svg\"\n"
	r, _ = ix.Check(ctx, "acme/omaphoto", &unknown)
	if c, _ := checkOf(r, "omastore.toml"); c.Status != CheckWarn {
		t.Errorf("unknown field: %+v", r.Checks)
	}
	if !r.Compatible() {
		t.Errorf("an unknown field only warns: %+v", r.Checks)
	}

	r, _ = ix.Check(ctx, "acme/omalib", nil)
	if c, _ := checkOf(r, "Installable"); c.Status != CheckFail || !strings.Contains(c.Detail, "omalib-0.1.deb") {
		t.Errorf("no binary: %+v", c)
	}

	gh.repos["acme/norel"] = &fakeRepo{repo: github.Repo{FullName: "acme/norel", Name: "norel", PushedAt: t0}, sha: "x"}
	r, _ = ix.Check(ctx, "acme/norel", nil)
	if c, _ := checkOf(r, "Release"); c.Status != CheckFail {
		t.Errorf("no release: %+v", r.Checks)
	}

	r, _ = ix.Check(ctx, "acme/missing", nil)
	if c, _ := checkOf(r, "Repository"); c.Status != CheckFail {
		t.Errorf("missing: %+v", r.Checks)
	}

	gh.rate = true
	if _, err := ix.Check(ctx, "acme/omaphoto", nil); err == nil {
		t.Error("a rate limit must be an error, not a report")
	}
}

func TestSuggestManifestKeepsDeclaredAssets(t *testing.T) {
	m := &manifest.Manifest{Kind: manifest.KindApp, Linux: map[string]manifest.Target{ArchAMD64: {Asset: "x"}}}
	if s := suggestManifest(&github.Repo{FullName: "a/b"}, store.App{}, nil, ArchAMD64, m, true); s != "" {
		t.Errorf("suggestion for a complete manifest: %q", s)
	}
	if got := tomlString("a\"b\\c\n"); got != `"a\"b\\c\u000A"` {
		t.Errorf("tomlString = %s", got)
	}
}
