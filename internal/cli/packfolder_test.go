package cli

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/zipfile"
)

// packFolders is a project requiring a resource pack and a shader built from folders, the pack
// placed under a filename of its own.
func packFolders(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	for rel, body := range map[string]string{
		"Resource Packs/Mod Menu Helper/pack.mcmeta":                    `{"pack":{"pack_format":64,"description":"helper"}}`,
		"Resource Packs/Mod Menu Helper/assets/modmenu/lang/en_us.json": `{"modmenu.title":"Mods"}`,
		"shaders/bsl/shaders/gbuffers_basic.vsh":                        "// bsl",
	} {
		writeProjectFile(t, h, rel, []byte(body))
	}
	writeProjectFile(t, h, "files/iris-1.9.jar", makeJar(t, "iris", "iris-1.9.jar", "client").data)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{
			"iris":            map[string]any{"file": "files/iris-1.9.jar"},
			"mod-menu-helper": map[string]any{"type": "resourcepack", "file": "Resource Packs/Mod Menu Helper", "filename": "Mod Menu Helper.zip"},
			"bsl":             map[string]any{"type": "shader", "file": "shaders/bsl"},
		}
	})
	return h
}

func folderZip(t *testing.T, h *harness, rel string) ([]byte, string) {
	t.Helper()
	data, err := zipfile.Folder(filepath.Join(h.dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(data)
	return data, hex.EncodeToString(sum[:])
}

func TestPackFolderLocksAndBuildsAsAZip(t *testing.T) {
	h := packFolders(t)
	h.mustRun(t, "lock")
	helper, helperSha := folderZip(t, h, "Resource Packs/Mod Menu Helper")
	bsl, bslSha := folderZip(t, h, "shaders/bsl")

	l := h.readLock(t)
	if got := l.ResourcePacks["mod-menu-helper"]; got.File != "Resource Packs/Mod Menu Helper" || got.Filename != "Mod Menu Helper.zip" || got.Sha512 != helperSha || got.Size != int64(len(helper)) {
		t.Fatalf("locked pack folder: %+v", got)
	}
	if got := l.Shaders["bsl"]; got.File != "shaders/bsl" || got.Filename != "bsl.zip" || got.Sha512 != bslSha || got.Loaders != nil {
		t.Fatalf("locked shader folder: %+v", got)
	}

	_, stderr := h.mustRunStderr(t, "install")
	if strings.Contains(stderr, "out of date") {
		t.Fatalf("install after lock: %s", stderr)
	}
	if got := readBuilt(t, h, "resourcepacks/Mod Menu Helper.zip"); got != string(helper) {
		t.Fatal("the pack folder is placed as its zip under its filename")
	}
	if got := readBuilt(t, h, "shaderpacks/bsl.zip"); got != string(bsl) {
		t.Fatal("the shader folder is placed as <key>.zip")
	}

	h.mustRun(t, "lock")
	if again := h.readLock(t).ResourcePacks["mod-menu-helper"].Sha512; again != helperSha {
		t.Fatalf("relocking the same folder moved its sha512: %s", again)
	}
}

func TestPackFolderStaleness(t *testing.T) {
	h := packFolders(t)
	h.mustRun(t, "lock")
	writeProjectFile(t, h, "Resource Packs/Mod Menu Helper/.DS_Store", []byte("junk"))
	writeProjectFile(t, h, "Resource Packs/Mod Menu Helper/assets/.git/HEAD", []byte("junk"))

	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "build", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.LockStale || len(env.Warnings) != 0 {
		t.Fatalf("excluded files make the lock stale: %+v", env)
	}

	dangling := filepath.Join(h.dir, "Resource Packs", "Mod Menu Helper", "gone.png")
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.png"), dangling); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "build", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "gone.png is a symlink to nothing") {
		t.Fatalf("a folder that can't be zipped: code=%d env=%+v", code, env)
	}
	if err := os.Remove(dangling); err != nil {
		t.Fatal(err)
	}

	writeProjectFile(t, h, "Resource Packs/Mod Menu Helper/assets/modmenu/lang/en_us.json", []byte(`{"modmenu.title":"Mod list"}`))
	code, stdout, _ = h.run(t, "build", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "mod-menu-helper: the file's bytes changed") {
		t.Fatalf("an edited file: code=%d env=%+v", code, env)
	}
	h.mustRun(t, "lock")
	helper, sha := folderZip(t, h, "Resource Packs/Mod Menu Helper")
	if got := h.readLock(t).ResourcePacks["mod-menu-helper"].Sha512; got != sha {
		t.Fatalf("lock adopts the edited folder: %s", got)
	}
	h.mustRun(t, "build")
	if got := readBuilt(t, h, "resourcepacks/Mod Menu Helper.zip"); got != string(helper) {
		t.Fatal("the build places the edited folder's zip")
	}
}

func TestPackFolderGoneOrUncached(t *testing.T) {
	h := packFolders(t)
	h.mustRun(t, "lock")
	helper, _ := folderZip(t, h, "Resource Packs/Mod Menu Helper")

	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "install")
	if got := readBuilt(t, h, "resourcepacks/Mod Menu Helper.zip"); got != string(helper) {
		t.Fatal("install re-zips a folder the cache lost")
	}

	if err := os.RemoveAll(filepath.Join(h.dir, "Resource Packs")); err != nil {
		t.Fatal(err)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "mod-menu-helper: Resource Packs/Mod Menu Helper is gone") {
		t.Fatalf("a gone folder is served from the cache with a warning: %+v", env)
	}
}

func TestFolderRefusedOffAPack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry map[string]any
		rel   string
		code  string
	}{
		{"mod", map[string]any{"file": "files/private.jar"}, "files/private.jar/fabric.mod.json", "file-not-found"},
		{"modpack", map[string]any{"type": "modpack", "file": "packs/base"}, "packs/base/modrinth.index.json", "file-not-found"},
		{"pack file", map[string]any{"type": "resourcepack", "file": "files/faithful.jar"}, "", "manifest-invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustRun(t, "create", "--loader", "fabric")
			if tc.rel != "" {
				writeProjectFile(t, h, tc.rel, []byte("{}"))
			} else {
				writeProjectFile(t, h, "files/faithful.jar", []byte("not a zip"))
			}
			h.editManifest(t, func(m map[string]any) {
				m["requires"] = map[string]any{"x": tc.entry}
			})
			code, stdout, _ := h.run(t, "lock", "--json")
			var env out.Envelope
			_ = json.Unmarshal([]byte(stdout), &env)
			if code == 0 || env.Error == nil || env.Error.Code != tc.code {
				t.Fatalf("code=%d env=%+v", code, env)
			}
		})
	}
}
