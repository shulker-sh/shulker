package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func writeFolder(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var (
	helperFiles = map[string]string{
		"pack.mcmeta":                    `{"pack":{"pack_format":64,"description":"helper"}}`,
		"assets/modmenu/lang/en_us.json": `{"modmenu.title":"Mods"}`,
	}
	shaderFiles = map[string]string{"shaders/gbuffers_basic.vsh": "// bsl"}
)

func TestAddFolderInsideTheProjectIsReferencedInPlace(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	writeFolder(t, filepath.Join(h.dir, "Resource Packs", "Mod Menu Helper"), helperFiles)
	writeFolder(t, filepath.Join(h.dir, "shaders", "bsl.v8"), shaderFiles)
	h.mustRun(t, "resourcepack", "add", "Resource Packs/Mod Menu Helper/")
	h.mustRun(t, "add", "shaders/bsl.v8")

	m := h.readManifest(t)
	if got := m.Requires["mod-menu-helper"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeResourcePack, File: "Resource Packs/Mod Menu Helper"}) {
		t.Fatalf("a folder inside the project is referenced where it lies: %+v", m.Requires)
	}
	if got := m.Requires["bsl.v8"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeShader, File: "shaders/bsl.v8"}) {
		t.Fatalf("a bare add reads a folder's kind and keys it by its whole name: %+v", m.Requires)
	}
	if _, err := os.Stat(filepath.Join(h.dir, manifest.FilesDir)); !os.IsNotExist(err) {
		t.Fatalf("nothing is copied into files/: %v", err)
	}
	_, helperSha := folderZip(t, h, "Resource Packs/Mod Menu Helper")
	_, bslSha := folderZip(t, h, "shaders/bsl.v8")
	l := h.readLock(t)
	if got := l.ResourcePacks["mod-menu-helper"]; got.File != "Resource Packs/Mod Menu Helper" || got.Sha512 != helperSha {
		t.Fatalf("the folder locks as its zip: %+v", got)
	}
	if got := l.Shaders["bsl.v8"]; got.Sha512 != bslSha || got.Filename != "bsl.v8.zip" {
		t.Fatalf("the shader folder locks as its zip: %+v", got)
	}
}

func TestAddFolderFromOutsideIsCopiedIntoFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	outside := filepath.Join(t.TempDir(), "BSL Shaders")
	writeFolder(t, outside, shaderFiles)
	writeFolder(t, outside, map[string]string{"shaders/old.fsh": "// old", ".git/HEAD": "ref: refs/heads/main", "shaders/.DS_Store": "junk"})
	_, stderr := h.mustRunStderr(t, "shader", "add", outside)

	if !strings.Contains(stderr, "copied BSL Shaders/ into files/") {
		t.Fatalf("add says where the folder went: %s", stderr)
	}
	if got := h.readManifest(t).Requires["bsl-shaders"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeShader, File: "files/BSL Shaders"}) {
		t.Fatalf("the entry names the copy: %+v", got)
	}
	if readProjectFile(t, h, "files/BSL Shaders/shaders/gbuffers_basic.vsh") != "// bsl" {
		t.Fatal("the folder is copied whole")
	}
	for _, left := range []string{".git", "shaders/.DS_Store"} {
		if _, err := os.Stat(filepath.Join(h.dir, "files", "BSL Shaders", filepath.FromSlash(left))); !os.IsNotExist(err) {
			t.Fatalf("%s, which the zip leaves out, is not copied: %v", left, err)
		}
	}
	_, sha := folderZip(t, h, "files/BSL Shaders")
	if got := h.readLock(t).Shaders["bsl-shaders"].Sha512; got != sha {
		t.Fatalf("the copy is locked: %s", got)
	}

	if err := os.Remove(filepath.Join(outside, "shaders", "old.fsh")); err != nil {
		t.Fatal(err)
	}
	writeFolder(t, outside, map[string]string{"shaders/gbuffers_basic.vsh": "// bsl 2"})
	h.mustRun(t, "shader", "add", outside)
	if readProjectFile(t, h, "files/BSL Shaders/shaders/gbuffers_basic.vsh") != "// bsl 2" {
		t.Fatal("a re-add refreshes the copy")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "files", "BSL Shaders", "shaders", "old.fsh")); !os.IsNotExist(err) {
		t.Fatalf("a file gone from the folder is gone from the copy: %v", err)
	}
	_, sha = folderZip(t, h, "files/BSL Shaders")
	if got := h.readLock(t).Shaders["bsl-shaders"].Sha512; got != sha {
		t.Fatalf("the re-add relocks: %s", got)
	}
}

func TestAddFolderInAnOwnedFolderIsCopied(t *testing.T) {
	h := newInPlace(t)
	writeFolder(t, filepath.Join(h.dir, "resourcepacks", "Helper"), helperFiles)
	h.mustRun(t, "resourcepack", "add", "resourcepacks/Helper", "--as", "menu-helper")

	if got := h.readManifest(t).Requires["menu-helper"]; !reflect.DeepEqual(got, manifest.Require{Type: manifest.TypeResourcePack, File: "files/Helper"}) {
		t.Fatalf("a folder in an in-place build folder is copied, keyed by --as: %+v", h.readManifest(t).Requires)
	}
	if readProjectFile(t, h, "files/Helper/pack.mcmeta") != helperFiles["pack.mcmeta"] {
		t.Fatal("the folder is copied into files/")
	}
}

func TestAddFolderRefusals(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	helper := filepath.Join(t.TempDir(), "Helper")
	writeFolder(t, helper, helperFiles)
	writeFolder(t, filepath.Join(h.dir, "files", "Helper"), map[string]string{"pack.mcmeta": `{"pack":{"pack_format":64,"description":"another"}}`})
	notes := filepath.Join(t.TempDir(), "notes")
	writeFolder(t, notes, map[string]string{"notes.txt": "hi"})
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"add", "--type", "mod", helper}, "usage"},
		{[]string{"mod", "add", helper}, "usage"},
		{[]string{"add", notes}, "type-ambiguous"},
		{[]string{"resourcepack", "add", helper}, "file-taken"},
	} {
		code, stdout, _ := h.run(t, append([]string{"--json"}, c.args...)...)
		if e := failureCode(t, stdout); code == 0 || e.Code != c.code {
			t.Errorf("%v: code=%d %+v", c.args, code, e)
		}
	}
	if !strings.Contains(readProjectFile(t, h, "files/Helper/pack.mcmeta"), "another") {
		t.Fatal("a different folder in files/ is never replaced")
	}
	if len(h.readManifest(t).Requires) != 0 {
		t.Fatalf("nothing is added: %+v", h.readManifest(t).Requires)
	}
}

func TestAddFolderThroughASymlinkCopiesItsTarget(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	target := filepath.Join(t.TempDir(), "Real")
	writeFolder(t, target, helperFiles)
	link := filepath.Join(t.TempDir(), "Helper")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "resourcepack", "add", link)
	if readProjectFile(t, h, "files/Helper/pack.mcmeta") != helperFiles["pack.mcmeta"] {
		t.Fatal("the folder a symlink names is copied under the symlink's name")
	}
}

func TestAddFolderThroughASymlinkToItsOwnCopyKeepsIt(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	writeFolder(t, filepath.Join(h.dir, "files", "Helper"), helperFiles)
	link := filepath.Join(t.TempDir(), "Helper")
	if err := os.Symlink(filepath.Join(h.dir, "files", "Helper"), link); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "resourcepack", "add", link)
	h.mustRun(t, "resourcepack", "add", link)
	if readProjectFile(t, h, "files/Helper/pack.mcmeta") != helperFiles["pack.mcmeta"] {
		t.Fatal("copying a folder onto itself keeps it")
	}
	if got := h.readManifest(t).Requires["helper"].File; got != "files/Helper" {
		t.Fatalf("the entry names the folder: %q", got)
	}
}

func TestAddFolderHoldingTheProjectIsRefused(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	code, stdout, _ := h.run(t, "--json", "resourcepack", "add", filepath.Dir(h.dir))
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
		t.Fatalf("a folder the project lies in can't be copied into it: code=%d %+v", code, e)
	}
	if _, err := os.Stat(filepath.Join(h.dir, manifest.FilesDir)); !os.IsNotExist(err) {
		t.Fatalf("nothing is copied: %v", err)
	}
}

func TestAddFolderFollowsSymlinksInsideWithAWarning(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	outside := filepath.Join(t.TempDir(), "Helper")
	writeFolder(t, outside, helperFiles)
	shared := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(shared, []byte("logo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(outside, "pack.png")); err != nil {
		t.Fatal(err)
	}
	_, stderr := h.mustRunStderr(t, "resourcepack", "add", outside)

	st, err := os.Lstat(filepath.Join(h.dir, "files", "Helper", "pack.png"))
	if err != nil || !st.Mode().IsRegular() || readProjectFile(t, h, "files/Helper/pack.png") != "logo" {
		t.Fatalf("the copy holds what the symlink points at: %v %v", st, err)
	}
	if strings.Contains(stderr, "symlink") {
		t.Fatalf("the copy has no symlink left to warn about: %s", stderr)
	}

	writeFolder(t, filepath.Join(h.dir, "packs", "Inside"), helperFiles)
	if err := os.Symlink(shared, filepath.Join(h.dir, "packs", "Inside", "pack.png")); err != nil {
		t.Fatal(err)
	}
	_, stderr = h.mustRunStderr(t, "resourcepack", "add", "packs/Inside")
	if !strings.Contains(stderr, "packs/Inside/pack.png is a symlink") {
		t.Fatalf("add warns about a symlink it follows: %s", stderr)
	}
	_, sha := folderZip(t, h, "packs/Inside")
	if got := h.readLock(t).ResourcePacks["inside"].Sha512; got != sha {
		t.Fatalf("the symlinked file is locked in the zip: %s", got)
	}
}
