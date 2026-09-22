package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
)

func TestBareSyncInProjectSyncsItsOwnEntries(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	plain := filepath.Join(t.TempDir(), "plain")
	h.mustRun(t, "sync", h.dir, "--into", plain)
	otherProject := t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "other", "-C", otherProject)
	h.mustRun(t, "sync", otherProject, "--into", filepath.Join(t.TempDir(), "other"))

	dirs := func(args ...string) string {
		t.Helper()
		var env struct {
			Data []syncInstanceResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, append(args, "--json")...)), &env); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range env.Data {
			got = append(got, r.Dir)
		}
		return strings.Join(got, "\n")
	}
	if got := dirs("sync"); got != gameDir+"\n"+plain && got != plain+"\n"+gameDir {
		t.Fatalf("bare sync in the project:\n%s", got)
	}
	if got := dirs("sync", "--launcher", "prism"); got != gameDir {
		t.Fatalf("--launcher narrows the project's entries:\n%s", got)
	}

	code, stdout, _ := h.run(t, "sync", "-C", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-instance" || len(e.Candidates) != 1 {
		t.Fatalf("outside a project, bare sync lists every registered instance: %d %s", code, stdout)
	}

	h.mustRun(t, "unlink", gameDir)
	if got := dirs("sync"); got != plain {
		t.Fatalf("an unlinked instance stays unlinked:\n%s", got)
	}
	if err := os.RemoveAll(registryPath(h)); err != nil {
		t.Fatal(err)
	}
	if got := dirs("sync"); got != plain {
		t.Fatalf("a recorded sync directory is synced with no registry at all:\n%s", got)
	}

	empty := t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "empty", "-C", empty)
	code, stdout, _ = h.run(t, "sync", "-C", empty, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-instances" {
		t.Fatalf("a project with nothing synced: %d %s", code, stdout)
	}
}

func TestProjectSyncGivesADetachedBuildItsDirectorysID(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--as", "client")
	packs := t.TempDir()
	foTest := filepath.Join(packs, "fo-test")
	h.mustRun(t, "sync", h.dir, "--into", foTest)

	ids := func(args ...string) map[string]string {
		t.Helper()
		var env struct {
			Data []syncInstanceResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, append(args, "--json")...)), &env); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, r := range env.Data {
			got[r.ID] = r.Dir
			if r.Detached != (r.ID != "client") {
				t.Fatalf("only a detached build is marked detached: %+v", r)
			}
		}
		return got
	}
	if got := ids("sync"); len(got) != 2 || got["client"] == "" || got["fo-test"] != foTest {
		t.Fatalf("the detached build takes its directory's id: %v", got)
	}
	syncedDir := func(id string) string {
		t.Helper()
		var env struct {
			Data syncResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "-i", id, "--json")), &env); err != nil {
			t.Fatal(err)
		}
		return env.Data.Dir
	}
	if got := syncedDir("fo-test"); got != foTest {
		t.Fatalf("sync -i fo-test picks the directory: %s", got)
	}

	shadow := filepath.Join(t.TempDir(), "client")
	h.mustRun(t, "sync", h.dir, "--into", shadow)
	if got := ids("sync"); got["client"] == shadow || got["client-2"] != shadow {
		t.Fatalf("a colliding detached build falls back to a suffixed id: %v", got)
	}
	if got := syncedDir("client"); got == shadow {
		t.Fatalf("a detached build can't shadow a row's id: %s", got)
	}
	if got := syncedDir("client-2"); got != shadow {
		t.Fatalf("sync -i client-2 picks the colliding directory: %s", got)
	}

	twin := filepath.Join(t.TempDir(), "fo-test")
	h.mustRun(t, "sync", h.dir, "--into", twin)
	if got := ids("sync"); got["fo-test"] != foTest || got["fo-test-2"] != twin {
		t.Fatalf("a second detached build in a same-named folder takes a suffixed id: %v", got)
	}
	if got := syncedDir("fo-test-2"); got != twin {
		t.Fatalf("sync -i fo-test-2 picks the second directory: %s", got)
	}

	text := h.mustRun(t, "sync")
	if !strings.Contains(text, "fo-test client (detached build) "+foTest) {
		t.Fatalf("the sync heading marks a detached build and shows its directory:\n%s", text)
	}
}

func TestInstanceHeadingMarksADetachedBuild(t *testing.T) {
	var theme out.Theme
	linked := instanceEntry{Instance: config.Instance{ID: "client", Name: "Pack", Launcher: "prism", Dir: "/p/client"}, Side: "client"}
	detached := instanceEntry{Instance: config.Instance{ID: "fo-test", Name: "Pack", Dir: "/packs/fo-test"}, Side: "client", detached: true}
	if got := instanceHeading(theme, linked); got != "Pack client client (Prism Launcher)" {
		t.Fatalf("linked heading = %q", got)
	}
	if got := instanceHeading(theme, detached); got != "Pack fo-test client (detached build) /packs/fo-test" {
		t.Fatalf("detached heading = %q", got)
	}
	if got := instancePickLabel(theme, linked); got != "Pack client client (Prism Launcher) /p/client" {
		t.Fatalf("linked picker line = %q", got)
	}
	if got := instancePickLabel(theme, detached); got != "Pack fo-test client (detached build) /packs/fo-test" {
		t.Fatalf("detached picker line = %q", got)
	}
}

func TestProjectSyncWithOnlyRowsHasNoDetachedBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", t.TempDir(), "--as", "client")

	var env struct {
		Data []syncInstanceResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || env.Data[0].ID != "client" || env.Data[0].Detached {
		t.Fatalf("a project whose synced directories all have rows lists just the rows: %+v", env.Data)
	}
	if text := h.mustRun(t, "sync"); strings.Contains(text, "detached build") {
		t.Fatalf("no entry is marked detached:\n%s", text)
	}
}
