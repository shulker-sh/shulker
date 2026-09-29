package build

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/build/marker"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/takedown"
)

// markerText is the description the client build's marker jar gives the mod list.
func (p *testProject) markerText() string {
	p.t.Helper()
	r, err := zip.OpenReader(p.builtPath("client", marker.JarPath(p.b.Manifest.Name)))
	if err != nil {
		p.t.Fatal(err)
	}
	defer r.Close()
	f, err := r.Open("fabric.mod.json")
	if err != nil {
		p.t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		p.t.Fatal(err)
	}
	var mod struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &mod); err != nil {
		p.t.Fatal(err)
	}
	return mod.Description
}

func TestReviewHasNothingToCompareOnAFirstBuild(t *testing.T) {
	p := newProject(t)
	p.save()

	c, err := p.b.Review("client", Options{})

	if err != nil || c != nil {
		t.Fatalf("a directory with no record reviews nothing: %+v %v", c, err)
	}
}

func TestReviewFindsAddedUnpublishedAndMovedEntries(t *testing.T) {
	p := newProject(t)
	sodium := p.modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2"))
	lithium := p.modrinth.Publish(provider.Project{ID: "gvQqBUqZ", Slug: "lithium", Title: "Lithium"}, provider.Version{ID: "LiThIuM1", Number: "0.14", File: provider.File{Filename: "lithium-0.14.jar"}}, modJar(t, "lithium", "0.14"))
	p.lockMod("sodium", p.modrinth, sodium)
	p.save()
	p.mustBuild("client", Options{})
	if st := instance.LoadState(p.builtPath("client", "")); len(st.Entries) != 1 || st.Entries[0] != (instance.LockedEntry{Key: "sodium", Type: "mod", Provider: "modrinth", Project: "AANobbMI"}) {
		t.Fatalf("a build records the lock's entries: %+v", st.Entries)
	}

	p.lockMod("lithium", p.modrinth, lithium)
	m := p.b.Lock.Mods["sodium"]
	m.Project = "ELSEWHERE"
	p.b.Lock.Mods["sodium"] = m
	p.override("mods/extra.jar", "unpublished")
	p.override("config/extra.json", "{}")
	p.save()

	c, err := p.b.Review("client", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Added) != 1 || c.Added[0].Key != "lithium" || c.Added[0].Provider != "modrinth" {
		t.Fatalf("added: %+v", c.Added)
	}
	if len(c.Moved) != 1 || c.Moved[0].Key != "sodium" || c.Moved[0].Project != "ELSEWHERE" || c.Moved[0].WasProject != "AANobbMI" {
		t.Fatalf("moved: %+v", c.Moved)
	}
	if len(c.Unpublished) != 1 || c.Unpublished[0].Path != "mods/extra.jar" {
		t.Fatalf("unpublished: only the override jar, not the config: %+v", c.Unpublished)
	}
}

func TestChangelogKeepsAMonthOfSyncs(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	old := instance.Changes{At: "2026-08-20T12:00:00Z", Added: []instance.Changed{{Key: "old"}}}
	recent := instance.Changes{At: "2026-09-10T12:00:00Z", Added: []instance.Changed{{Key: "recent"}}}
	latest := &instance.Changes{At: "2026-09-29T12:00:00Z", Added: []instance.Changed{{Key: "latest"}}}

	got := changelog([]instance.Changes{old, recent}, latest, now)
	if len(got) != 2 || got[0].At != recent.At || got[1].At != latest.At {
		t.Fatalf("changelog: %+v", got)
	}
	if got := changelog([]instance.Changes{recent}, &instance.Changes{At: latest.At}, now); len(got) != 1 {
		t.Fatalf("a sync that brought nothing adds nothing: %+v", got)
	}
}

func TestMarkerShowsNoticesOnlyWhileThereIsSomethingToShow(t *testing.T) {
	p := newProject(t)
	sodium := p.modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2"))
	p.lockMod("sodium", p.modrinth, sodium)
	p.save()
	p.mustBuild("client", Options{})
	if text := p.markerText(); strings.Contains(text, "Notices") || strings.Contains(text, "Synced") {
		t.Fatalf("nothing to show, no notices: %q", text)
	}

	jar := "mods/" + sodium.File.Filename
	if err := os.WriteFile(p.builtPath("client", jar), []byte("stage3"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := takedown.File{Entry: takedown.Entry{Key: "sodium", Type: "mod", Provider: "modrinth", Project: "AANobbMI", Version: "QANobbMI", Sha512: sodium.File.Sha512}, Status: takedown.Gone}
	changes := &instance.Changes{At: time.Now().UTC().Format(time.RFC3339), Added: []instance.Changed{{Key: "lithium", Provider: "modrinth"}}, Unpublished: []instance.Changed{{Path: "mods/extra.jar"}}, Moved: []instance.Changed{{Key: "jei", Project: "NEW", WasProject: "OLD"}}}
	p.mustBuild("client", Options{Takedowns: &instance.Takedowns{CheckedAt: changes.At, Files: []takedown.File{gone}}, Changes: changes})

	text := p.markerText()
	day := time.Now().UTC().Format(time.DateOnly)
	for _, want := range []string{"Notices", "sodium is gone from Modrinth", jar + " changed since shulker placed it", "Synced " + day, "Added lithium", "Added mods/extra.jar", "jei moved to project NEW"} {
		if !strings.Contains(text, want) {
			t.Errorf("marker description lacks %q:\n%s", want, text)
		}
	}
}
