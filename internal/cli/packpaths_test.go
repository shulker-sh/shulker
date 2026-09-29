package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
)

var escapingIndexPaths = []string{"../../ESCAPED.txt", "/tmp/ESCAPED.txt", `\ESCAPED.txt`, "C:/ESCAPED.txt", "mods/CON.jar", "config/nul"}

// escapingPack rewrites a good pack at rel so one index file names path.
func escapingPack(t *testing.T, h *harness, rel, path string) {
	t.Helper()
	archivePack(t, h, rel, "v1")
	file := filepath.Join(h.dir, filepath.FromSlash(rel))
	rewriteMrpack(t, file, file, func(index *mrpackIndex, _ map[string][]byte) {
		index.Files[0].Path = path
	})
}

func assertNothingEscaped(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{root, filepath.Dir(root), filepath.Dir(filepath.Dir(root))} {
		if _, err := os.Stat(filepath.Join(dir, "ESCAPED.txt")); err == nil {
			t.Fatalf("written outside the project: %s", dir)
		}
	}
}

func TestImportRefusesAnIndexPathOutsideThePack(t *testing.T) {
	for _, path := range escapingIndexPaths {
		t.Run(path, func(t *testing.T) {
			h := archiveProject(t)
			escapingPack(t, h, "evil.mrpack", path)
			dest := filepath.Join(t.TempDir(), "a", "b", "proj")
			code, stdout, _ := h.run(t, "import", "-C", dest, filepath.Join(h.dir, "evil.mrpack"), "--json")
			if code == 0 || failureCode(t, stdout).Code != "mrpack-invalid" {
				t.Fatalf("code=%d %s", code, stdout)
			}
			assertNothingEscaped(t, dest)
		})
	}
}

func TestAddRefusesAnIndexPathOutsideThePack(t *testing.T) {
	for _, path := range escapingIndexPaths {
		t.Run(path, func(t *testing.T) {
			h := archiveProject(t)
			escapingPack(t, h, "packs/evil.mrpack", path)
			code, stdout, _ := h.run(t, "add", filepath.Join(h.dir, "packs", "evil.mrpack"), "--json")
			if code == 0 || failureCode(t, stdout).Code != "mrpack-invalid" {
				t.Fatalf("code=%d %s", code, stdout)
			}
			if _, ok := h.readManifest(t).Requires["someone"]; ok {
				t.Fatal("a refused archive stays out of the manifest")
			}
		})
	}
}

func TestBuildRefusesALockedPackWhoseIndexLeavesThePack(t *testing.T) {
	for _, path := range escapingIndexPaths {
		t.Run(path, func(t *testing.T) {
			h := archiveProject(t)
			archivePack(t, h, "packs/someone.mrpack", "v1")
			h.editManifest(t, func(m map[string]any) {
				m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
			})
			h.mustRun(t, "lock")
			h.mustRun(t, "install")
			escapingPack(t, h, "packs/someone.mrpack", path)
			pinLockTo(t, h, "someone", "packs/someone.mrpack")
			code, stdout, _ := h.run(t, "build", "--json")
			if code == 0 || failureCode(t, stdout).Code != "mrpack-invalid" {
				t.Fatalf("code=%d %s", code, stdout)
			}
			assertNothingEscaped(t, filepath.Join(h.dir, "build", "client"))
		})
	}
}

// pinLockTo points a modpack's lock entry at the archive's current bytes, as a lock written before
// the archive was refused would.
func pinLockTo(t *testing.T, h *harness, name, rel string) {
	t.Helper()
	file := filepath.Join(h.dir, filepath.FromSlash(rel))
	sha, err := fsutil.SHA512(file)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	h.editLock(t, func(l *lock.Lock) {
		pin := l.Modpacks[name]
		pin.Sha512, pin.Size = sha, st.Size()
		l.Modpacks[name] = pin
	})
}

func TestImportRefusesAnIndexDownloadOffTheMrpackHosts(t *testing.T) {
	for download, from := range map[string]string{"https://evil.example/sodium.jar": "https://evil.example", "http://cdn.modrinth.com/cdn/sodium.jar": "http://cdn.modrinth.com"} {
		t.Run(download, func(t *testing.T) {
			h := archiveProject(t)
			offHostPack(t, h, "evil.mrpack", download)
			code, stdout, _ := h.run(t, "import", "-C", filepath.Join(t.TempDir(), "proj"), filepath.Join(h.dir, "evil.mrpack"), "--json")
			if code == 0 || failureCode(t, stdout).Code != "mrpack-invalid" || !strings.Contains(stdout, from) {
				t.Fatalf("code=%d %s", code, stdout)
			}
		})
	}
}

func TestBuildRefusesALockedPackWithAnIndexDownloadOffTheMrpackHosts(t *testing.T) {
	h := archiveProject(t)
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	offHostPack(t, h, "packs/someone.mrpack", "https://evil.example/sodium.jar")
	pinLockTo(t, h, "someone", "packs/someone.mrpack")
	code, stdout, _ := h.run(t, "build", "--json")
	if code == 0 || failureCode(t, stdout).Code != "mrpack-invalid" || !strings.Contains(stdout, "https://evil.example") {
		t.Fatalf("code=%d %s", code, stdout)
	}
}

// offHostPack writes a good pack at rel whose first index file downloads from download.
func offHostPack(t *testing.T, h *harness, rel, download string) {
	t.Helper()
	archivePack(t, h, rel, "v1")
	file := filepath.Join(h.dir, filepath.FromSlash(rel))
	rewriteMrpack(t, file, file, func(index *mrpackIndex, _ map[string][]byte) {
		index.Files[0].Downloads = []string{download}
	})
}
