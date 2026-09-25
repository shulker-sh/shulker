package cli

import "testing"

func TestModrinthPackLocksTheProvidersSha1(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "fresh-animations")
	if got, want := h.readLock(t).ResourcePacks["fresh-animations"].Sha1, h.jars["fresh-animations"].sha1; got != want {
		t.Fatalf("sha1 = %q, want the provider's %q", got, want)
	}
}

func TestPackWithNoProviderSha1LocksAComputedOne(t *testing.T) {
	h := newHarness(t)
	h.modrinthNoSha1 = true
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	if got, want := h.readLock(t).Shaders["complementary-reimagined"].Sha1, h.jars["complementary"].sha1; got != want {
		t.Fatalf("sha1 = %q, want the file's own %q", got, want)
	}
}

func TestPackCopiedFromAModpackKeepsItsSha1(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "fresh-animations")
	lockedPack(t, h, h.dir+"/base", `"fresh-animations": {"type": "resourcepack"}`)
	h.mustRun(t, "remove", "fresh-animations")
	h.mustRun(t, "modpack", "add", "./base")

	got := h.readLock(t).ResourcePacks["fresh-animations"]
	if got.Modpack != "base" || got.Sha1 != h.jars["fresh-animations"].sha1 {
		t.Fatalf("copied pack: %+v", got)
	}
}

func TestLocalPackHasNoSha1(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	pack := makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"faithful"}}`)
	writeProjectFile(t, h, "packs/faithful.zip", pack.data)
	h.mustRun(t, "resourcepack", "add", "packs/faithful.zip")
	if got := h.readLock(t).ResourcePacks["faithful"]; got.Sha512 != pack.sha512 || got.Sha1 != "" {
		t.Fatalf("local pack: %+v", got)
	}
}
