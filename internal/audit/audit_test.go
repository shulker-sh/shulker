package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
)

type fixture struct {
	t    *testing.T
	e    *envtest.Env
	p    *project.Project
	jars map[string][]byte
}

// newFixture is a fabric client project locking sodium and lithium from the fake Modrinth, both
// published on day 1.
func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, e: envtest.New(t), jars: map[string][]byte{}}
	f.p = &project.Project{
		Dir:      t.TempDir(),
		Manifest: &manifest.Manifest{Name: "pack", Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric"}, Client: &manifest.Client{}, Requires: map[string]manifest.Require{}},
		Lock:     lock.New(),
	}
	f.lockMod("sodium")
	f.lockMod("lithium")
	return f
}

func (f *fixture) lockMod(slug string) {
	data := envtest.ModJar(f.t, slug, "1.0", "*")
	v := f.e.Modrinth.Publish(provider.Project{ID: slug + "-id", Slug: slug}, provider.Version{Number: "1.0", Published: envtest.Day(1), File: provider.File{Filename: slug + "-1.0.jar"}}, data)
	f.jars[slug] = data
	f.p.Manifest.Requires[slug] = manifest.Require{Provider: "modrinth"}
	f.p.Lock.Mods[slug] = lock.Mod{
		Provider: "modrinth", Project: v.ProjectID, Version: v.ID, VersionNumber: v.Number, Published: v.Published,
		Filename: v.File.Filename, URL: &v.File.URL, Sha512: v.File.Sha512, Side: "both", RequiredBy: []string{}, Aliases: lock.Aliases{},
	}
}

func (f *fixture) write(rel string, data []byte) {
	f.t.Helper()
	path := filepath.Join(f.p.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) run(o Options) *Report {
	f.t.Helper()
	if o.Now.IsZero() {
		o.Now = envtest.Day(30)
	}
	r, err := Run(context.Background(), build.New(f.e.Env, f.p, nil), o)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestACleanLockHasNoFindings(t *testing.T) {
	r := newFixture(t).run(Options{MinReleaseAge: 7 * 24 * time.Hour})
	if r.Fails() || len(r.Provenance)+len(r.Unpublished)+len(r.Installed)+len(r.Young) > 0 {
		t.Fatalf("expected nothing: %+v", r)
	}
}

func TestAnEntryOffItsProvidersHostsFailsTheAudit(t *testing.T) {
	f := newFixture(t)
	m := f.p.Lock.Mods["sodium"]
	elsewhere := "https://evil.example/sodium.jar"
	m.URL = &elsewhere
	f.p.Lock.Mods["sodium"] = m

	r := f.run(Options{})
	if !r.Fails() || !slices.Equal(r.Provenance, []build.Mismatch{{Key: "sodium", Provider: "modrinth", Host: "evil.example"}}) {
		t.Fatalf("provenance: %+v", r.Provenance)
	}
}

func TestFilesNoProviderPublishedAreListedWithWhereTheyCameFrom(t *testing.T) {
	f := newFixture(t)
	f.p.Lock.Mods["handmade"] = lock.Mod{File: "jars/handmade.jar", Filename: "handmade.jar", Sha512: "aa", Side: "both", RequiredBy: []string{}, Aliases: lock.Aliases{}}
	f.write("overrides/mods/extra.jar", []byte("jar"))
	f.write("client-overrides/resourcepacks/look.zip", []byte("zip"))
	f.write("overrides/config/extra.toml", []byte("not a jar"))

	r := f.run(Options{})
	want := []Unpublished{
		{Key: "handmade", Path: "mods/handmade.jar", From: FromFile, Source: "jars/handmade.jar"},
		{Path: "mods/extra.jar", From: FromOverride, Source: "overrides"},
		{Path: "resourcepacks/look.zip", From: FromOverride, Source: "client-overrides"},
	}
	if r.Fails() || !slices.Equal(r.Unpublished, want) {
		t.Fatalf("unpublished: %+v", r.Unpublished)
	}
}

func TestInstalledJarsAreCheckedAgainstTheLock(t *testing.T) {
	f := newFixture(t)
	sodium := f.p.Lock.Mods["sodium"].Filename
	f.write("build/client/mods/"+sodium, append(f.jars["sodium"], "tampered"...))
	f.write("build/client/mods/"+f.p.Lock.Mods["lithium"].Filename, f.jars["lithium"])
	f.write("build/client/mods/stray.jar", []byte("stray"))
	dir := filepath.Join(f.p.Dir, "build", "client")

	r := f.run(Options{Dirs: []Dir{{Path: dir, Side: "client"}}})
	want := []Installed{
		{Dir: dir, Path: "mods/" + sodium, Key: "sodium", Problem: Changed},
		{Dir: dir, Path: "mods/stray.jar", Problem: Unlisted},
	}
	if r.Fails() || !slices.Equal(r.Installed, want) {
		t.Fatalf("installed: %+v", r.Installed)
	}
}

func TestVersionsYoungerThanTheReleaseAgeAreListed(t *testing.T) {
	f := newFixture(t)
	m := f.p.Lock.Mods["lithium"]
	m.Published = envtest.Day(28)
	f.p.Lock.Mods["lithium"] = m

	r := f.run(Options{MinReleaseAge: 7 * 24 * time.Hour})
	if r.Fails() || len(r.Young) != 1 || r.Young[0].Key != "lithium" || r.Young[0].AgeDays != 2 || r.MinReleaseAge != 7 {
		t.Fatalf("young: %+v", r)
	}
}

func TestNamedKeysNarrowEveryCheck(t *testing.T) {
	f := newFixture(t)
	for _, key := range []string{"sodium", "lithium"} {
		m := f.p.Lock.Mods[key]
		elsewhere := "https://evil.example/" + key + ".jar"
		m.URL, m.Published = &elsewhere, envtest.Day(29)
		f.p.Lock.Mods[key] = m
		f.write("build/client/mods/"+m.Filename, []byte("changed"))
	}
	f.write("overrides/mods/extra.jar", []byte("jar"))
	f.write("build/client/mods/stray.jar", []byte("stray"))

	r := f.run(Options{Keys: []string{"lithium"}, Dirs: []Dir{{Path: filepath.Join(f.p.Dir, "build", "client"), Side: "client"}}, MinReleaseAge: 7 * 24 * time.Hour})
	if len(r.Provenance) != 1 || r.Provenance[0].Key != "lithium" || len(r.Young) != 1 || r.Young[0].Key != "lithium" ||
		len(r.Installed) != 1 || r.Installed[0].Key != "lithium" || len(r.Unpublished) != 0 {
		t.Fatalf("narrowed to lithium: %+v", r)
	}
}

func TestANamedKeyTheLockDoesntHoldIsRefused(t *testing.T) {
	f := newFixture(t)
	_, err := Run(context.Background(), build.New(f.e.Env, f.p, nil), Options{Keys: []string{"sodum"}})
	if e := out.AsError(err); e.Code != "mod-not-found" || !slices.Contains(e.Candidates, "sodium") {
		t.Fatalf("expected mod-not-found: %v", err)
	}
}

func TestAFileItsProviderNoLongerHasIsATakedown(t *testing.T) {
	f := newFixture(t)
	f.e.Modrinth.Files = slices.DeleteFunc(f.e.Modrinth.Files, func(v provider.Version) bool { return v.ProjectID == "lithium-id" })

	r := f.run(Options{})
	if !r.Fails() || len(r.Takedowns) != 1 || r.Takedowns[0].Key != "lithium" || len(r.Moved) != 0 || f.e.Modrinth.Requests["Filed"] != 1 {
		t.Fatalf("takedowns: %+v, requests %v", r.Takedowns, f.e.Modrinth.Requests)
	}
}

func TestAFileItsProviderFilesUnderAnotherProjectFailsProvenance(t *testing.T) {
	f := newFixture(t)
	m := f.p.Lock.Mods["lithium"]
	m.Project = "not-lithium"
	f.p.Lock.Mods["lithium"] = m

	r := f.run(Options{})
	if !r.Fails() || len(r.Moved) != 1 || r.Moved[0].Key != "lithium" || r.Moved[0].FiledUnder != "lithium-id" || len(r.Takedowns) != 0 {
		t.Fatalf("moved: %+v", r)
	}
}

func TestATakedownCheckThatCantReachItsProviderIsSkippedNotPassed(t *testing.T) {
	f := newFixture(t)
	f.e.Modrinth.FiledErr = errors.New("--offline")

	r := f.run(Options{})
	if r.Fails() || len(r.Skipped) != 1 || r.Skipped[0].Provider != "modrinth" {
		t.Fatalf("skipped: %+v", r)
	}
}
