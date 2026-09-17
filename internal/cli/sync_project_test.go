package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareSyncInProjectSyncsItsOwnEntries(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	plain := filepath.Join(t.TempDir(), "plain")
	h.mustRun(t, "sync", h.dir, "--into", plain, "--name", "friends")
	otherProject := t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "other", "-C", otherProject)
	h.mustRun(t, "sync", otherProject, "--into", filepath.Join(t.TempDir(), "other"), "--name", "other")

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
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-instance" || len(e.Candidates) != 3 {
		t.Fatalf("outside a project, bare sync lists every entry: %d %s", code, stdout)
	}

	h.mustRun(t, "unlink", plain)
	if got := dirs("sync"); got != gameDir {
		t.Fatalf("an unlinked directory stays unlinked:\n%s", got)
	}
	h.mustRun(t, "sync", h.dir, "--into", plain, "--name", "friends")
	if err := os.Remove(registryPath(h)); err != nil {
		t.Fatal(err)
	}
	if got := dirs("sync"); got != gameDir+"\n"+plain && got != plain+"\n"+gameDir {
		t.Fatalf("recorded sync directories without registry entries are still synced:\n%s", got)
	}

	empty := t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "empty", "-C", empty)
	code, stdout, _ = h.run(t, "sync", "-C", empty, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-instances" {
		t.Fatalf("a project with nothing synced: %d %s", code, stdout)
	}
}
