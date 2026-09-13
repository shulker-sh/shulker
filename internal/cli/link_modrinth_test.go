package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/launcher"
)

func fakeModrinthDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, launcher.ModrinthDBFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, launcher.ModrinthProfilesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeModrinthApp stands in for the app taking an .mrpack: it records the pack and
// makes the instance folder the app would, with the app's " (1)" rule when taken.
func fakeModrinthApp(t *testing.T, launcherDir string, packs *[]string) func(string) error {
	return func(path string) error {
		t.Helper()
		*packs = append(*packs, path)
		index, _ := readMrpack(t, path)
		folder := filepath.Join(launcherDir, launcher.ModrinthProfilesDir, index.Name)
		if _, err := os.Stat(folder); err == nil {
			folder += " (1)"
		}
		return os.MkdirAll(filepath.Join(folder, "mods"), 0o755)
	}
}

func TestLinkModrinth(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "set", "version", "1.0")

	launcherDir := fakeModrinthDir(t)
	var packs []string
	h.openFile = fakeModrinthApp(t, launcherDir, &packs)

	var env struct {
		Data modrinthReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "modrinth", "--launcher-dir", launcherDir, "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	instDir := filepath.Join(launcherDir, launcher.ModrinthProfilesDir, "pack")
	rep := env.Data
	if !rep.Created || rep.InstanceDir != instDir || rep.Name != "pack" || rep.Target != "client" || rep.Source != h.dir || rep.Sync != nil || rep.Pack != filepath.Join(h.cache, "mrpack", "shulker-pack.mrpack") {
		t.Fatalf("link report: %+v", rep)
	}
	if len(packs) != 1 || packs[0] != rep.Pack {
		t.Fatalf("packs opened: %v", packs)
	}
	index, entries := readMrpack(t, rep.Pack)
	if _, bundled := entries["overrides/mods/"+h.jars["sodium"].filename]; index.Name != "pack" || index.VersionID != "1.0" || !bundled {
		t.Fatalf("exported pack: %+v, entries %d", index, len(entries))
	}
	links := readLinks(t, h)
	if len(links) != 1 || links[0].Launcher != "modrinth" || links[0].LauncherDir != launcherDir || links[0].Dir != instDir || links[0].Name != "pack" || links[0].Target != "client" {
		t.Fatalf("registry: %+v", links)
	}
	if _, err := os.Stat(filepath.Join(instDir, build.StateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("linking must leave the install to the app, not sync over it: %v", err)
	}
	stdout = h.mustRun(t, "link", "modrinth", "--launcher-dir", launcherDir)
	if !strings.Contains(stdout, "updated instance pack") || !strings.Contains(stdout, "synced client") {
		t.Fatalf("relink should sync the instance in place: %s", stdout)
	}
	if len(packs) != 1 {
		t.Fatalf("relink must not open the app again: %v", packs)
	}
	if _, err := os.Stat(filepath.Join(instDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("relink should sync mods into the instance: %v", err)
	}
	if st := build.LoadState(instDir); st.Source != h.dir || st.Target != "client" {
		t.Fatalf("state origin: %+v", st)
	}
	stdout = h.mustRun(t, "links")
	if !strings.Contains(stdout, "Modrinth App") || !strings.Contains(stdout, instDir) {
		t.Fatalf("links: %s", stdout)
	}

	h.mustRun(t, "link", "modrinth", "--launcher-dir", launcherDir, "--name", "Friends", "--json")
	if len(packs) != 2 {
		t.Fatalf("a new name is a new instance: %v", packs)
	}
	if index, _ := readMrpack(t, packs[1]); index.Name != "Friends" {
		t.Fatalf("the pack carries --name: %+v", index)
	}
	links = readLinks(t, h)
	if len(links) != 2 || links[1].Name != "Friends" || links[1].Dir != filepath.Join(launcherDir, launcher.ModrinthProfilesDir, "Friends") {
		t.Fatalf("registry after --name: %+v", links)
	}

	other := t.TempDir()
	if err := os.CopyFS(other, os.DirFS(h.dir)); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "link", "modrinth", other, "--launcher-dir", launcherDir, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-exists" || !strings.Contains(e.Message, h.dir) {
		t.Fatalf("linking another source into the instance: exit %d %s", code, stdout)
	}
	h.mustRun(t, "link", "modrinth", other, "--launcher-dir", launcherDir, "--force")
	if links := readLinks(t, h); links[0].Source != other || links[0].Dir != instDir {
		t.Fatalf("--force should repoint the instance: %+v", links[0])
	}

	stdout = h.mustRun(t, "unlink", "pack", "--launcher", "modrinth")
	if !strings.Contains(stdout, "stays in the app") || !strings.Contains(stdout, "$ shulker link modrinth "+other+" --target client --name pack --launcher-dir "+launcherDir) {
		t.Fatalf("unlink modrinth: %s", stdout)
	}
	if _, err := os.Stat(instDir); err != nil {
		t.Fatalf("unlink must leave the instance alone: %v", err)
	}
}

func TestLinkModrinthTakenFolderAndAdoptedSync(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	launcherDir := fakeModrinthDir(t)
	taken := filepath.Join(launcherDir, launcher.ModrinthProfilesDir, "pack")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	var packs []string
	h.openFile = fakeModrinthApp(t, launcherDir, &packs)
	var env struct {
		Data modrinthReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "modrinth", "--launcher-dir", launcherDir, "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.InstanceDir != taken+" (1)" {
		t.Fatalf("the folder the app made should be recorded: %+v", env.Data)
	}
	if index, _ := readMrpack(t, packs[0]); index.VersionID != time.Now().UTC().Format("2006.01.02") {
		t.Fatalf("a pack without a version is dated: %+v", index)
	}

	h.mustRun(t, "sync", h.dir, "--target", "client", "--into", taken, "--name", "By hand")
	links := readLinks(t, h)
	if len(links) != 2 || links[1].Launcher != "modrinth" || links[1].LauncherDir != launcherDir || links[1].Name != "By hand" {
		t.Fatalf("a sync into a profiles folder should land under Modrinth App: %+v", links)
	}
}

func TestLinkModrinthErrors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "link", "modrinth", "--launcher-dir", filepath.Join(t.TempDir(), "missing"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-not-found" {
		t.Fatalf("missing launcher: exit %d %s", code, stdout)
	}

	launcherDir := fakeModrinthDir(t)
	expected := filepath.Join(launcherDir, launcher.ModrinthProfilesDir, "pack")
	h.openFile = func(string) error { return errors.New("no app") }
	code, _, stderr := h.run(t, "link", "modrinth", "--launcher-dir", launcherDir)
	if code == 0 || !strings.Contains(stderr, "(open-failed)") || !strings.Contains(stderr, "no app") || !strings.Contains(stderr, "$ shulker sync "+h.dir+" --target client --into "+launcher.ShellArg(expected)+" --name pack") {
		t.Fatalf("opener failure: exit %d %s", code, stderr)
	}

	h.openFile = func(string) error { return nil }
	h.waitFor = 50 * time.Millisecond
	code, _, stderr = h.run(t, "link", "modrinth", "--launcher-dir", launcherDir)
	if code == 0 || !strings.Contains(stderr, "(instance-not-created)") || !strings.Contains(stderr, "$ shulker sync "+h.dir+" --target client --into "+launcher.ShellArg(expected)) {
		t.Fatalf("no instance appears: exit %d %s", code, stderr)
	}
	if links := readLinks(t, h); len(links) != 0 {
		t.Fatalf("nothing should be registered: %+v", links)
	}

	code, stdout, _ = h.run(t, "link", "modrinth", "--launcher-dir", launcherDir, "--ref", "main", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--ref without a source: exit %d %s", code, stdout)
	}
}

func TestLinkModrinthFromRemoteSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir

	launcherDir := fakeModrinthDir(t)
	var packs []string
	h.openFile = fakeModrinthApp(t, launcherDir, &packs)
	var env struct {
		Data modrinthReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "modrinth", source, "--launcher-dir", launcherDir, "--name", "Friends", "--ref", "main", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	instDir := filepath.Join(launcherDir, launcher.ModrinthProfilesDir, "Friends")
	if rep := env.Data; rep.Source != source || rep.Ref != "main" || rep.InstanceDir != instDir || !rep.Created {
		t.Fatalf("link report: %+v", rep)
	}
	index, _ := readMrpack(t, packs[0])
	if index.Name != "Friends" || len(index.VersionID) != 12 {
		t.Fatalf("a git pack is versioned by its commit: %+v", index)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("linking a remote source must not build in the current directory: %v", err)
	}
	if links := readLinks(t, h); len(links) != 1 || links[0].Source != source || links[0].Ref != "main" {
		t.Fatalf("registry: %+v", links)
	}
	h.mustRun(t, "sync", "--instance", "Friends")
	if st := build.LoadState(instDir); st.Source != source || st.Ref != "main" {
		t.Fatalf("state origin after sync: %+v", st.Origin)
	}
}
