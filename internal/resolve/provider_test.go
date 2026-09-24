package resolve

import (
	"context"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

func fakeResolver(providers ...*fake.Provider) *Resolver {
	r := &Resolver{Manifest: &manifest.Manifest{Requires: map[string]manifest.Require{}}, Lock: lock.New(), Providers: provider.Providers{}}
	for _, p := range providers {
		r.Providers[p.Name()] = p
		r.Manifest.Providers = append(r.Manifest.Providers, p.Name())
	}
	return r
}

func TestLookupSkipsAnUnavailableProviderAndKeepsTheMissHelp(t *testing.T) {
	alpha, beta := fake.New("alpha"), fake.New("beta")
	beta.Unavailable = out.Errorf("provider-unavailable", "beta needs a key")
	r := fakeResolver(alpha, beta)
	_, _, err := r.lookup(context.Background(), "shiny", "", "")
	e := out.AsError(err)
	if e.Code != "mod-not-found" || e.Message != "shiny was not found on alpha" || len(e.Items) != 1 || e.Items[0] != "skipped: beta needs a key" {
		t.Fatalf("got %+v", e)
	}
	alpha.Known = []provider.Project{{ID: "p1", Slug: "shiny", Type: manifest.TypeMod}}
	p, proj, err := r.lookup(context.Background(), "shiny", "", "")
	if err != nil || p.Name() != "alpha" || proj.ID != "p1" {
		t.Fatalf("got %v %+v %v", p, proj, err)
	}
	if _, _, err := r.lookup(context.Background(), "shiny", "gamma", ""); out.CodeOf(err) != "provider-unavailable" || !strings.Contains(out.AsError(err).Message, "not a known provider") {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestSetSourceRecordsTheIDUnlessTheSlugIsAsGoodAKey(t *testing.T) {
	alpha, beta := fake.New("alpha"), fake.New("beta")
	r := fakeResolver(alpha, beta)
	proj := &provider.Project{ID: "p1", Slug: "shiny"}
	var entry manifest.Require
	r.setSource(&entry, "shiny", alpha, proj)
	if entry.Project != "" || entry.Provider != "" {
		t.Errorf("first provider by slug: %+v", entry)
	}
	entry = manifest.Require{}
	r.setSource(&entry, "shine", alpha, proj)
	if entry.Project != "p1" {
		t.Errorf("another key: %+v", entry)
	}
	entry = manifest.Require{}
	r.setSource(&entry, "shiny", beta, proj)
	if entry.Provider != "beta" {
		t.Errorf("second provider: %+v", entry)
	}
}

func TestPageForAsksTheProviderForTheProjectPage(t *testing.T) {
	r := fakeResolver(fake.New("alpha"))
	if got := r.pageFor(lock.Mod{Provider: "alpha", Project: "p1"}); got != "https://alpha.test/project/p1" {
		t.Errorf("no url: %s", got)
	}
	url := "https://cdn.alpha.test/p1.jar"
	if got := r.pageFor(lock.Mod{Provider: "alpha", Project: "p1", URL: &url}); got != url {
		t.Errorf("with a url: %s", got)
	}
	if got := r.pageFor(lock.Mod{Provider: "alpha", Project: "p1", Page: "https://alpha.test/mod/shiny/files/1"}); got != "https://alpha.test/mod/shiny/files/1" {
		t.Errorf("with a page: %s", got)
	}
}

func TestIdentifyAsksEachProviderInManifestOrder(t *testing.T) {
	alpha, beta := fake.New("alpha"), fake.New("beta")
	alpha.Unavailable = out.Errorf("provider-unavailable", "alpha needs a key")
	beta.Known = []provider.Project{{ID: "p1", Slug: "shiny", Type: manifest.TypeMod}}
	beta.Files = []provider.Version{{ID: "v1", ProjectID: "p1", File: provider.File{Sha1: "86f7e437faa5a7fce15d1ddcb9eaeaea377667b8", Filename: "shiny.jar", URL: "https://beta.test/shiny.jar"}}}
	r := fakeResolver(alpha, beta)
	im := newImporter(r, &mrpack.Archive{}, false)
	im.toIdentify(mrpack.Override{Layer: "overrides", Path: "mods/shiny.jar", Data: []byte("a")}, "")
	im.toIdentify(mrpack.Override{Layer: "overrides", Path: "mods/other.jar", Data: []byte("b")}, "")
	if err := im.identify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if alpha.Requests["Identify"] != 0 || beta.Requests["Identify"] != 1 {
		t.Errorf("requests: alpha %v, beta %v", alpha.Requests, beta.Requests)
	}
	h, ok := im.found["overrides/mods/shiny.jar"]
	if len(im.found) != 1 || !ok || h.p.Name() != "beta" || h.v.ID != "v1" {
		t.Errorf("found %+v", im.found)
	}
	if len(im.rep.Warnings) != 1 || !strings.Contains(im.rep.Warnings[0], "2 file(s) weren't looked up on Alpha (alpha needs a key)") {
		t.Errorf("warnings %v", im.rep.Warnings)
	}
}
