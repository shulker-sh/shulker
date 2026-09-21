package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type modpackLockView struct {
	Modpacks map[string]struct {
		Locked     bool   `json:"locked"`
		LockSha256 string `json:"lockSha256"`
		Sha256     string `json:"sha256"`
	} `json:"modpacks"`
	Mods map[string]struct {
		Modpack       string `json:"modpack"`
		VersionNumber string `json:"versionNumber"`
	} `json:"mods"`
}

// lockedPack builds a modpack directory whose shulker.lock is a copy of the
// project's own, so its entries name the same fake provider and its platform
// matches exactly.
func lockedPack(t *testing.T, h *harness, dir, mods string) {
	t.Helper()
	writePrismPack(t, dir, "~26.2", mods, nil)
	copyLock(t, h.dir, dir)
}

func copyLock(t *testing.T, fromDir, toDir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fromDir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toDir, "shulker.lock"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func editPackLock(t *testing.T, dir string, edit func(m map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, "shulker.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// project sets up a project that has resolved sodium, then hands the resulting
// lock to a modpack directory and drops the mod from the project itself.
func projectWithLockedPack(t *testing.T, name string) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	dir := filepath.Join(h.dir, name)
	lockedPack(t, h, dir, `"sodium": {}`)
	h.mustRun(t, "remove", "sodium")
	return h, dir
}

func TestLockedModpackCopiesItsLock(t *testing.T) {
	h, _ := projectWithLockedPack(t, "base")
	h.mustRun(t, "modpack", "add", "./base")

	var l modpackLockView
	h.readJSON(t, "shulker.lock", &l)
	if got := l.Modpacks["base"]; !got.Locked || got.LockSha256 == "" {
		t.Fatalf("the modpack entry should record locked and lockSha256: %+v", got)
	}
	for _, id := range []string{"sodium", "fabric-api"} {
		if got := l.Mods[id].Modpack; got != "base" {
			t.Fatalf("%s should be marked as coming from the modpack, got %q", id, got)
		}
	}
}

func TestUnlockedModpackResolvesItsModsHere(t *testing.T) {
	h, _ := projectWithLockedPack(t, "base")
	h.mustRun(t, "modpack", "add", "./base", "--unlocked")

	var l modpackLockView
	h.readJSON(t, "shulker.lock", &l)
	if l.Modpacks["base"].Locked {
		t.Fatalf("--unlocked should not record locked: %+v", l.Modpacks["base"])
	}
	if got := l.Mods["sodium"].Modpack; got != "" {
		t.Fatalf("a floating modpack's mods are resolved here, so they carry no origin, got %q", got)
	}
}

func TestLockedModpackNeedsAnExactPlatform(t *testing.T) {
	h, dir := projectWithLockedPack(t, "base")
	editPackLock(t, dir, func(m map[string]any) { m["minecraft"] = "26.1" })

	code, stdout, _ := h.run(t, "modpack", "add", "./base", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-mismatch" {
		t.Fatalf("a locked modpack on another Minecraft should be refused: code=%d %s", code, stdout)
	}
}

func TestModpackLockedWithoutALockFails(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "base"), "~26.2", `"sodium": {}`, nil)
	h.mustRun(t, "modpack", "add", "./base")

	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["base"].(map[string]any)["locked"] = true
	})
	code, stdout, _ := h.run(t, "lock", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-lock-missing" {
		t.Fatalf("locked: true without a lock at the source should fail: code=%d %s", code, stdout)
	}
}

func TestLockedModpacksPinningOneModDifferently(t *testing.T) {
	h, base := projectWithLockedPack(t, "base")
	other := filepath.Join(h.dir, "other")
	writePrismPack(t, other, "~26.2", `"sodium": {}`, nil)
	copyLock(t, base, other)
	editPackLock(t, other, func(m map[string]any) {
		mods := m["mods"].(map[string]any)
		sodium := mods["sodium"].(map[string]any)
		sodium["sha512"] = strings.Repeat("a", 128)
		sodium["versionNumber"] = "9.9.9+mc26.2"
	})
	h.mustRun(t, "modpack", "add", "./base")

	code, stdout, _ := h.run(t, "modpack", "add", "./other", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-conflict" {
		t.Fatalf("two locked modpacks pinning one mod differently should be refused: code=%d %s", code, stdout)
	}
}

func TestUpdateRefusesALockedModpacksMod(t *testing.T) {
	h, _ := projectWithLockedPack(t, "base")
	h.mustRun(t, "modpack", "add", "./base")

	code, stdout, _ := h.run(t, "update", "sodium", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-provided" {
		t.Fatalf("a mod a locked modpack pins is not updatable here: code=%d %s", code, stdout)
	}
}

// The manifest and the lock are both cached, so a build off a locked URL modpack needs
// no network, and a prune keeps what the project's lock pins.
func TestURLModpackWithALockIsLocked(t *testing.T) {
	h, dir := projectWithLockedPack(t, "base")
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()
	source := srv.URL + "/shulker.json"

	h.mustRun(t, "modpack", "add", source)

	var l modpackLockView
	h.readJSON(t, "shulker.lock", &l)
	got := l.Modpacks["base"]
	if !got.Locked || got.LockSha256 == "" || got.Sha256 == "" {
		t.Fatalf("a URL modpack with a lock beside it should be locked: %+v", got)
	}
	for _, id := range []string{"sodium", "fabric-api"} {
		if from := l.Mods[id].Modpack; from != "base" {
			t.Fatalf("%s should come from the modpack, got %q", id, from)
		}
	}

	h.mustRun(t, "cache", "prune")
	srv.Close()
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatal(err)
	}
}

func TestURLModpackWithoutALockStaysFloating(t *testing.T) {
	h, dir := projectWithLockedPack(t, "base")
	if err := os.Remove(filepath.Join(dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	h.mustRun(t, "modpack", "add", srv.URL+"/shulker.json")

	var l modpackLockView
	h.readJSON(t, "shulker.lock", &l)
	if got := l.Modpacks["base"]; got.Locked || got.LockSha256 != "" {
		t.Fatalf("a URL modpack with no lock beside it should stay floating: %+v", got)
	}
	if from := l.Mods["sodium"].Modpack; from != "" {
		t.Fatalf("a floating modpack's mods are resolved here, so they carry no origin, got %q", from)
	}
}
