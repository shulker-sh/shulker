package resolve

import (
	"context"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// urlHosts is a Modrinth with sodium and fresh-animations beside a CurseForge with jei at two
// files, the newer one current.
func urlHosts(t *testing.T) (modrinth, cf *host) {
	t.Helper()
	c := newCDN(t)
	modrinth = newHost(c, "modrinth")
	modrinth.publish(mod("AANobbMI", "sodium"), provider.Version{ID: "QANobbMI", Number: "1.0.0+mc26.2", File: provider.File{Filename: "sodium-1.0.0.jar"}}, modJar(t, "sodium", "1.0.0", "client"))
	fresh := provider.Project{ID: "50dA9Sha", Slug: "fresh-animations", Type: manifest.TypeResourcePack}
	modrinth.publish(fresh, provider.Version{ID: "FreshV194", Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "FreshAnimations_v1.9.4.zip"}}, zipFiles(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34,"description":"fresh"}}`}))
	cf = newHost(c, "curseforge").likeCurseForge()
	cf.publish(mod("394468", "sodium"), provider.Version{ID: "5000020", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2", "client"))
	cf.publish(mod("238222", "jei"), provider.Version{ID: "5000001", Number: "1.0.0", Published: day(1), File: provider.File{Filename: "jei-1.0.0.jar"}}, modJar(t, "jei", "1.0.0", "*"))
	cf.publish(mod("238222", "jei"), provider.Version{ID: "5000002", Number: "1.1.0", Published: day(5), File: provider.File{Filename: "jei-1.1.0.jar"}}, modJar(t, "jei", "1.1.0", "*"))
	return modrinth, cf
}

func fromURL(t *testing.T, h *harness, u provider.Ref, opts AddOptions) (string, AddOptions) {
	t.Helper()
	slug, opts, err := h.r.FromURL(context.Background(), u, opts)
	if err != nil {
		t.Fatal(err)
	}
	return slug, opts
}

func TestFromURLAddsEachProjectPinnedToTheVersionItsURLNames(t *testing.T) {
	modrinth, cf := urlHosts(t)
	h := newHarness(t, modrinth, cf)

	for _, u := range []provider.Ref{
		{Provider: "curseforge", Project: "238222", Version: "5000001"},
		{Provider: "modrinth", Project: "sodium", Version: "1.0.0+mc26.2"},
		{Provider: "modrinth", Project: "fresh-animations"},
	} {
		slug, opts := fromURL(t, h, u, AddOptions{})
		if opts.Provider != u.Provider || !opts.IsFromURL {
			t.Fatalf("%+v: %+v", u, opts)
		}
		h.mustAdd(slug, opts)
	}
	for key, want := range map[string]string{"jei": "5000001", "sodium": "QANobbMI"} {
		if got := h.mod(key).Version; got != want {
			t.Fatalf("%s locked at %s, want %s", key, got, want)
		}
		if got := h.r.Manifest.Mods()[key].Pin; got != want {
			t.Fatalf("%s pinned to %s, want %s", key, got, want)
		}
	}
	if h.mod("jei").Provider != "curseforge" {
		t.Fatalf("jei should come from curseforge: %+v", h.mod("jei"))
	}
	if _, ok := h.r.Lock.ResourcePacks["fresh-animations"]; !ok {
		t.Fatalf("the resource pack URL should add a resource pack: %+v", h.r.Lock.ResourcePacks)
	}
	if pin := h.r.Manifest.Requires["fresh-animations"].Pin; pin != "" {
		t.Fatalf("a project URL is no pin: %v", pin)
	}
}

func TestFromURLRefusesOptionsThatDisagreeWithTheURL(t *testing.T) {
	modrinth, cf := urlHosts(t)
	h := newHarness(t, modrinth, cf)
	for _, c := range []struct {
		u    provider.Ref
		opts AddOptions
	}{
		{provider.Ref{Provider: "modrinth", Project: "sodium"}, AddOptions{Provider: "curseforge"}},
		{provider.Ref{Provider: "curseforge", Project: "238222", Version: "5000001"}, AddOptions{Pin: "5000000"}},
	} {
		_, _, err := h.r.FromURL(context.Background(), c.u, c.opts)
		if e := out.AsError(err); e == nil || e.Code != "usage" {
			t.Fatalf("%+v with %+v: %v, want a usage error", c.u, c.opts, err)
		}
	}
	if _, _, err := h.r.FromURL(context.Background(), provider.Ref{Provider: "modrinth", Project: "sodium", Version: "1.0.0+mc26.2"}, AddOptions{Provider: "modrinth", Pin: "QANobbMI"}); err != nil {
		t.Fatalf("options that agree pass: %v", err)
	}
	if _, _, err := h.r.FromURL(context.Background(), provider.Ref{Provider: "curseforge", Project: "238222", Version: "5000009"}, AddOptions{}); out.CodeOf(err) != "version-not-found" {
		t.Fatalf("a version the URL names must exist: %v", err)
	}
}

func TestPinURLTakesOnlyAVersionOfTheLockedProject(t *testing.T) {
	modrinth, cf := urlHosts(t)
	h := newHarness(t, modrinth, cf)
	h.mustAdd("238222", AddOptions{Provider: "curseforge"})
	if got := h.mod("jei").Version; got != "5000002" {
		t.Fatalf("jei locked at %s, want the newer file", got)
	}
	ctx := context.Background()

	if _, err := h.r.PinURL(ctx, "nope", provider.Ref{Provider: "modrinth", Project: "sodium", Version: "QANobbMI"}); out.CodeOf(err) != "mod-not-found" {
		t.Fatalf("pinning a key the manifest lacks: %v", err)
	}
	for _, u := range []provider.Ref{
		{Provider: "modrinth", Project: "sodium", Version: "QANobbMI"},
		{Provider: "curseforge", Project: "394468", Version: "5000020"},
		{Provider: "curseforge", Project: "238222"},
	} {
		if _, err := h.r.PinURL(ctx, "jei", u); out.CodeOf(err) != "usage" {
			t.Fatalf("%+v: %v, want a usage error", u, err)
		}
	}

	if _, err := h.r.PinURL(ctx, "jei", provider.Ref{Provider: "curseforge", Project: "238222", Version: "5000001"}); err != nil {
		t.Fatal(err)
	}
	if got := h.mod("jei").Version; got != "5000001" {
		t.Fatalf("jei locked at %s, want 5000001", got)
	}
}
