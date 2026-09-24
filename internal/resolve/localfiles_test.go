package resolve

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
)

// writeProjectFile puts data at rel under the project, making the folders on the way.
func (h *harness) writeProjectFile(rel string, data []byte) {
	h.t.Helper()
	path := filepath.Join(h.r.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func privateModJar(t *testing.T, version string) []byte {
	t.Helper()
	return fabricJar(t, `{"id":"private-mod","version":"`+version+`","environment":"client","depends":{"fabricloader":">=0.17","fabric-api":"*"}}`, nil)
}

func packZip(t *testing.T, description string) []byte {
	t.Helper()
	return zipFiles(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34,"description":"` + description + `"}}`})
}

// localFiles is a project requiring a hand-written local mod, resource pack and shader, with the
// mod depending on fabric-api, which only the provider has. The project is not locked yet.
func localFiles(t *testing.T) (*harness, []byte) {
	t.Helper()
	alpha := newHost(newCDN(t), "alpha")
	alpha.publish(mod("a-fapi", "fabric-api"), provider.Version{Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, modJar(t, "fabric-api", "1.0.0", "*"))
	h := newHarness(t, alpha)
	jar := privateModJar(t, "1.4.0")
	h.writeProjectFile("files/private-mod-1.4.jar", jar)
	h.writeProjectFile("files/faithful.zip", packZip(t, "faithful"))
	h.writeProjectFile("files/bsl.zip", zipFiles(t, map[string]string{"shaders/gbuffers_basic.vsh": "// bsl"}))
	h.r.Manifest.Requires = map[string]manifest.Require{
		"private-mod": {File: "files/private-mod-1.4.jar"},
		"faithful":    {Type: manifest.TypeResourcePack, File: "files/faithful.zip"},
		"bsl":         {Type: manifest.TypeShader, File: "files/bsl.zip"},
	}
	return h, jar
}

func TestReconcileLocksLocalFilesByTheirBytes(t *testing.T) {
	h, jar := localFiles(t)
	h.mustReconcile()

	m := h.mod("private-mod")
	if m.File != "files/private-mod-1.4.jar" || m.Filename != "private-mod-1.4.jar" || m.Sha512 != sha512Hex(jar) || m.Size != int64(len(jar)) || m.Side != "client" {
		t.Fatalf("local mod entry: %+v", m)
	}
	if m.Provider != "" || m.Project != "" || m.Version != "" || m.VersionNumber != "" || m.URL != nil || m.Page != "" || m.Channel != "" {
		t.Fatalf("a local file entry names no provider: %+v", m)
	}
	faithful, bsl := h.r.Lock.ResourcePacks["faithful"], h.r.Lock.Shaders["bsl"]
	if faithful.File != "files/faithful.zip" || faithful.Provider != "" || faithful.Sha512 == "" || bsl.File != "files/bsl.zip" || bsl.Loaders != nil {
		t.Fatalf("local packs: %+v %+v", faithful, bsl)
	}
	if dep := h.mod("fabric-api"); dep.Provider != "alpha" || dep.RequiredBy[0] != "private-mod" {
		t.Fatalf("the local jar's dependency resolves from a provider: %+v", dep)
	}
}

func TestOutdatedAndUpdateLeaveLocalFilesAlone(t *testing.T) {
	h, _ := localFiles(t)
	h.mustReconcile()
	ctx := context.Background()

	for _, ids := range [][]string{nil, {"private-mod"}, {"faithful"}} {
		if got, err := h.r.Outdated(ctx, ids); err != nil || len(got) != 0 {
			t.Fatalf("outdated %v: %v %v", ids, got, err)
		}
	}
	for _, ids := range [][]string{nil, {"private-mod"}, {"bsl"}} {
		before := h.r.Snapshot()
		if err := h.r.Update(ctx, ids); err != nil {
			t.Fatalf("update %v: %v", ids, err)
		}
		if c := h.r.Changes(before); !c.IsEmpty() {
			t.Fatalf("update %v moved a local file: %+v", ids, c)
		}
	}
}

func TestPinRefusesLocalFiles(t *testing.T) {
	h, _ := localFiles(t)
	h.mustReconcile()
	ctx := context.Background()

	for _, tc := range []struct {
		key, version string
		unpin        bool
	}{{key: "private-mod"}, {key: "private-mod", version: "abc"}, {key: "private-mod", unpin: true}, {key: "faithful"}, {key: "bsl", unpin: true}} {
		var err error
		if tc.unpin {
			err = h.r.Unpin(ctx, tc.key)
		} else {
			_, err = h.r.Pin(ctx, tc.key, tc.version)
		}
		if e := out.AsError(err); e == nil || e.Code != "local-file" || e.Message != tc.key+" is a local file; there is no provider version to pin" {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestReconcileAdoptsALocalFilesNewBytes(t *testing.T) {
	h, _ := localFiles(t)
	h.mustReconcile()
	pack := packZip(t, "faithful 2")
	jar := privateModJar(t, "1.5.0")
	h.writeProjectFile("files/faithful.zip", pack)
	h.writeProjectFile("files/private-mod-1.4.jar", jar)

	if diffs := project.ZipEntryDifferences(h.r.Dir, "faithful", h.r.Manifest.Requires["faithful"], h.r.Lock.ResourcePacks["faithful"]); len(diffs) != 1 {
		t.Fatalf("changed bytes are staleness: %v", diffs)
	}
	h.mustReconcile()
	if got := h.r.Lock.ResourcePacks["faithful"]; got.Sha512 != sha512Hex(pack) || got.Size != int64(len(pack)) {
		t.Fatalf("the lock adopts the pack's new bytes: %+v", got)
	}
	if got := h.mod("private-mod"); got.Sha512 != sha512Hex(jar) || got.Size != int64(len(jar)) {
		t.Fatalf("the lock adopts the jar's new bytes: %+v", got)
	}
}

func TestReconcileKeepsAGoneLocalFileWhileTheCacheHasIt(t *testing.T) {
	h, jar := localFiles(t)
	h.mustReconcile()
	if err := os.Remove(filepath.Join(h.r.Dir, "files", "private-mod-1.4.jar")); err != nil {
		t.Fatal(err)
	}

	h.mustReconcile()
	if got := h.mod("private-mod"); got.Sha512 != sha512Hex(jar) {
		t.Fatalf("the entry the cache serves is kept: %+v", got)
	}
	if want := []string{"private-mod: files/private-mod-1.4.jar is gone; using the copy in the cache"}; !reflect.DeepEqual(h.r.Warnings, want) {
		t.Fatalf("warnings: %q", h.r.Warnings)
	}
	h.mustReconcile()
	if len(h.r.Warnings) != 1 {
		t.Fatalf("a gone file warns once: %q", h.r.Warnings)
	}

	if err := os.RemoveAll(h.r.Cache.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile(); out.CodeOf(err) != "local-file-missing" {
		t.Fatalf("reconcile with neither file nor cache: %v", err)
	}
}

func TestALocalJarLocksUnderTheKeyItIsListedAs(t *testing.T) {
	h, _ := localFiles(t)
	delete(h.r.Manifest.Requires, "private-mod")
	h.r.Manifest.Requires["mine"] = manifest.Require{File: "files/private-mod-1.4.jar", Side: "both"}

	h.mustReconcile()
	if mine := h.mod("mine"); mine.ModID != "private-mod" || mine.Side != "both" || h.mod("fabric-api").RequiredBy[0] != "mine" {
		t.Fatalf("a local jar under another key: %+v", mine)
	}

	jar := privateModJar(t, "1.5.0")
	h.writeProjectFile("files/private-mod-1.4.jar", jar)
	h.mustReconcile()
	if mine := h.mod("mine"); mine.Sha512 != sha512Hex(jar) || mine.ModID != "private-mod" {
		t.Fatalf("the lock adopts the new jar under the same key: %+v", mine)
	}
}
