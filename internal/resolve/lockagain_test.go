package resolve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

func TestLockAgainRestoresTheProvidersFileAtTheLockedVersion(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	h.mustAdd("jei", AddOptions{})
	want, err := json.Marshal(h.r.Lock)
	if err != nil {
		t.Fatal(err)
	}

	tampered := h.mod("sodium")
	elsewhere := "https://example.com/sodium.jar"
	tampered.URL, tampered.Sha512, tampered.Filename, tampered.Size = &elsewhere, strings.Repeat("0", 128), "sodium.jar", 1
	h.r.Lock.Mods["sodium"] = tampered
	h.nextCommand()

	if err := h.r.LockAgain(context.Background(), []string{"sodium"}); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(h.r.Lock)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("lock after lock sodium:\n%s\nwant:\n%s", got, want)
	}
}

func TestLockAgainPointsAGoneVersionAtUpdate(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	gone := h.mod("sodium")
	gone.Version = "a-gone"
	h.r.Lock.Mods["sodium"] = gone

	err := h.r.LockAgain(context.Background(), []string{"sodium"})
	if e := out.AsError(err); e == nil || e.Code != "version-not-found" || !strings.Contains(e.Help, "shulker update sodium") {
		t.Fatalf("expected version-not-found naming update, got %v", err)
	}
}

func TestLockAgainRefusesALocalFile(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	local := h.mod("sodium")
	local.File = "files/sodium.jar"
	h.r.Lock.Mods["sodium"] = local

	err := h.r.LockAgain(context.Background(), []string{"sodium"})
	if e := out.AsError(err); e == nil || e.Code != "local-file" {
		t.Fatalf("expected local-file, got %v", err)
	}
}

func TestLockAgainRestoresAPacksFile(t *testing.T) {
	alpha, _, h := twoHosts(t)
	alpha.Publish(provider.Project{ID: "fresh", Slug: "fresh-animations", Type: manifest.TypeResourcePack}, provider.Version{Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "fresh-animations-1.9.4.zip"}}, zipFiles(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34,"description":"fresh"}}`}))
	h.mustAdd("fresh-animations", AddOptions{})
	want := h.r.Lock.ResourcePacks["fresh-animations"]

	tampered := want
	elsewhere := "https://example.com/fresh.zip"
	tampered.URL, tampered.Sha512, tampered.Sha1 = &elsewhere, strings.Repeat("0", 128), strings.Repeat("0", 40)
	h.r.Lock.ResourcePacks["fresh-animations"] = tampered
	h.nextCommand()

	if err := h.r.LockAgain(context.Background(), []string{"fresh-animations"}); err != nil {
		t.Fatal(err)
	}
	got := h.r.Lock.ResourcePacks["fresh-animations"]
	if *got.URL != *want.URL || got.Sha512 != want.Sha512 || got.Sha1 != want.Sha1 || got.Filename != want.Filename {
		t.Fatalf("pack after lock:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestLockAgainRefusesAnEntryAModpackBrings(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	provided := h.mod("sodium")
	provided.Modpack = "base"
	h.r.Lock.Mods["sodium"] = provided
	h.r.Manifest.Requires["base"] = manifest.Require{Type: manifest.TypeModpack, Source: "https://example.com/base.git"}

	err := h.r.LockAgain(context.Background(), []string{"sodium"})
	if e := out.AsError(err); e == nil || e.Code != "modpack-provided" || !strings.Contains(e.Help, "its author") {
		t.Fatalf("expected modpack-provided naming the author, got %v", err)
	}
	h.r.Manifest.Requires["base"] = manifest.Require{Type: manifest.TypeModpack, Provider: "alpha", Project: "base"}
	err = h.r.LockAgain(context.Background(), []string{"sodium"})
	if e := out.AsError(err); e == nil || e.Code != "modpack-provided" || !strings.Contains(e.Help, "shulker lock base") {
		t.Fatalf("expected modpack-provided naming lock base, got %v", err)
	}
}
