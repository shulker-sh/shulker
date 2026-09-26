package cli

import (
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
	if !strings.Contains(stdout, "Created instance pack") || !strings.Contains(stdout, "Playing pack") {
		t.Fatalf("play creates the instance and launches it:\n%s", stdout)
	}
	waitForFile(t, filepath.Join(gameDir, "args.txt"))
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
