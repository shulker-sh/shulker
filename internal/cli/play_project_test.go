package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectPlayHarness is a project with a store of its own, left with the harness in its folder.
func projectPlayHarness(t *testing.T, h *harness) (root string) {
	t.Helper()
	root = shulkerInstances(t, h)
	h.mustRun(t, "config", "set", "store", filepath.Join(t.TempDir(), "store"))
	h.mustRun(t, "create", "--name", "pack")
	return root
}

func TestPlayInAProjectLaunchesItsOnlyShulkerInstance(t *testing.T) {
	h := newHarness(t)
	root := projectPlayHarness(t, h)
	h.mustRun(t, "link", "shulker")
	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir(), "--name", "Friends")

	rep := playJSON(t, h, "play", "--dry-run")

	if rep.Instance != "pack" || rep.GameDir != filepath.Join(root, "pack") {
		t.Fatalf("the project's own instance plays, not prism's: %+v", rep)
	}
}

func TestPlayInAProjectWithNoInstanceFailsWithoutATerminal(t *testing.T) {
	h := newHarness(t)
	projectPlayHarness(t, h)
	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir(), "--name", "Friends")

	code, stdout, _ := h.run(t, "play", "--dry-run", "--json")

	if code == 0 || !strings.Contains(stdout, `"instance-not-found"`) || !strings.Contains(stdout, "shulker link shulker") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestPlayInAFolderThatIsNeitherInstanceNorProjectSaysSo(t *testing.T) {
	h := newHarness(t)
	projectPlayHarness(t, h)
	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir(), "--name", "Friends")
	bare := t.TempDir()

	h.dir = ""

	for _, args := range [][]string{{"play", "-i", bare, "--dry-run", "--json"}, {"play", "-C", bare, "--dry-run", "--json"}} {
		code, stdout, _ := h.run(t, args...)

		if code == 0 || !strings.Contains(stdout, "is not a registered instance") || !strings.Contains(stdout, "shulker instances") || strings.Contains(stdout, "shulker link shulker") {
			t.Fatalf("%v: a folder with no project can't be linked, so the help lists the instances instead: %s", args, stdout)
		}
	}
}

func TestPlayInAProjectWithNoInstanceOffersToCreateOne(t *testing.T) {
	h := newHarness(t)
	root := projectPlayHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	code, stdout, stderr, s := h.runAnswering(t, map[string]string{"Create a shulker instance for pack and play it?": "yes"}, "play")

	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	if len(s.asked) != 1 {
		t.Fatalf("asked %v", s.asked)
	}
	gameDir := filepath.Join(root, "pack")
	if !strings.Contains(stdout, "Created instance pack") || !strings.Contains(stdout, "Launched pack as Notch") {
		t.Fatalf("play creates the instance and launches it:\n%s", stdout)
	}
	waitForFile(t, filepath.Join(gameDir, "args.txt"))
}

func TestPlayYesCreatesTheInstanceWithoutAsking(t *testing.T) {
	h := newHarness(t)
	root := projectPlayHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	code, stdout, stderr := h.run(t, "play", "--yes")

	if code != 0 || !strings.Contains(stdout, "Created instance pack") {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	waitForFile(t, filepath.Join(root, "pack", "args.txt"))
}

func TestPlayInAProjectWithNoInstanceCreatesNothingWhenDeclined(t *testing.T) {
	h := newHarness(t)
	root := projectPlayHarness(t, h)

	code, _, _, _ := h.runAnswering(t, map[string]string{"Create a shulker instance for pack and play it?": "no"}, "play")

	if code == 0 {
		t.Fatal("declining launches nothing")
	}
	if _, err := os.Stat(filepath.Join(root, "pack")); !os.IsNotExist(err) {
		t.Fatalf("declining creates nothing: %v", err)
	}
}

func TestPlayInAProjectWithSeveralInstancesAsksWhich(t *testing.T) {
	h := newHarness(t)
	root := projectPlayHarness(t, h)
	h.mustRun(t, "link", "shulker")
	h.mustRun(t, "link", "shulker", "--as", "second")

	code, stdout, stderr, _ := h.runAnswering(t, map[string]string{"Play which one?": "second"}, "play", "--dry-run")
	if code != 0 || !strings.Contains(stdout, filepath.Join(root, "second")) {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}

	code, stdout, _ = h.run(t, "play", "--dry-run", "--json")
	var env struct {
		Error struct {
			Code       string   `json:"code"`
			Candidates []string `json:"candidates"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if code == 0 || env.Error.Code != "ambiguous-instance" || len(env.Error.Candidates) != 2 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestPlayWaitSaysItCreatedTheInstanceBeforeItLaunched(t *testing.T) {
	h := newHarness(t)
	projectPlayHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	var both bytes.Buffer
	a := h.newApp(&both, &both)

	code := a.run(context.Background(), []string{"play", "--yes", "--wait"})

	got := both.String()
	created, launched := strings.Index(got, "Created instance pack"), strings.Index(got, "Launched pack as Notch")
	if code != 0 || created < 0 || launched < created {
		t.Fatalf("exit %d: the instance is made before the game launches:\n%s", code, got)
	}
}
