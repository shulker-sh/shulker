package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
)

type savesRunEnv[T any] struct {
	OK    bool          `json:"ok"`
	Data  []savesRun[T] `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func savesRunOf[T any](t *testing.T, h *harness, want int, args ...string) savesRunEnv[T] {
	t.Helper()
	code, stdout, stderr := h.run(t, append(args, "--json")...)
	if code != want {
		t.Fatalf("%v: exit %d, want %d: %s %s", args, code, want, stdout, stderr)
	}
	var env savesRunEnv[T]
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %v: %s", args, err, stdout)
	}
	return env
}

// fourRows registers two shulker instances sharing group default, a Prism instance keeping its
// own worlds, and a MultiMC instance with none, and returns the Prism game directory.
func fourRows(t *testing.T, h *harness) string {
	t.Helper()
	root := savesRoot(t, h)
	shulkerInstances(t, h)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "link", "shulker")
	h.mustRun(t, "link", "shulker", "--as", "pack2")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	h.mustRun(t, "link", "multimc", h.dir, "--launcher-dir", t.TempDir(), "--name", "Empty")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	addWorld(t, filepath.Join(root, "default"), "survival")
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })
	return gameDir
}

func runsByKey[T any](t *testing.T, runs []savesRun[T]) map[string]savesRun[T] {
	t.Helper()
	byKey := map[string]savesRun[T]{}
	for _, r := range runs {
		key := r.Group
		if key == "" {
			key = strings.Join(r.Instances, ",")
		}
		if _, dup := byKey[key]; dup {
			t.Fatalf("%s acted on twice: %+v", key, runs)
		}
		byKey[key] = r
	}
	return byKey
}

func TestBackupAllTakesEachSharedGroupOnce(t *testing.T) {
	h := newHarness(t)
	gameDir := fourRows(t, h)

	env := savesRunOf[backupResult](t, h, 0, "backup", "--all")
	runs := runsByKey(t, env.Data)
	if len(runs) != 3 {
		t.Fatalf("three targets, the group once: %+v", env.Data)
	}
	group := runs["default"]
	if !group.OK || group.Result == nil || group.Result.Worlds != 1 || strings.Join(group.Instances, ",") != "pack,pack2" || strings.Contains(group.Result.ID, "pack") {
		t.Fatalf("the shared group is backed up once, naming no instance: %+v", group)
	}
	friends := runs["friends"]
	if !friends.OK || friends.Result == nil || filepath.Dir(friends.Result.Path) != filepath.Join(gameDir, instance.Dir, "backups") {
		t.Fatalf("prism keeps its own: %+v", friends)
	}
	empty := runs["empty"]
	if !empty.OK || empty.Result != nil || !strings.Contains(empty.Skipped, "no worlds in") {
		t.Fatalf("a worldless row is skipped, not failed: %+v", empty)
	}

	stdout := h.mustRun(t, "backup", "--all")
	for _, want := range []string{"default save group (pack, pack2)", "Friends friends client (Prism Launcher)", "Backed up survival", "No worlds in"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("backup --all lacks %q: %s", want, stdout)
		}
	}

	if env := savesRunOf[backupResult](t, h, 0, "backup", "--all", "--launcher", "prism"); len(env.Data) != 1 || env.Data[0].Instances[0] != "friends" {
		t.Fatalf("--launcher narrows --all: %+v", env.Data)
	}
	project := h.dir
	h.dir = ""
	if got := backupOf(t, h, "-i", "pack", "--launcher", "shulker"); got.Group != "default" || !strings.Contains(got.ID, "pack") {
		t.Fatalf("--launcher narrows -i: %+v", got)
	}
	h.dir = project
	for _, args := range [][]string{
		{"backup", "--launcher", "prism"},
		{"backup", "--all", "--group", "default"},
		{"backup", "--all", "--side", "sideways"},
	} {
		if env := h.runSetting(t, 2, args...); env.Error == nil || env.Error.Code != "usage" {
			t.Fatalf("%v: %+v", args, env.Error)
		}
	}
	if env := h.runSetting(t, 1, "backup", "--all", "--side", "server"); env.Error == nil || env.Error.Code != "instance-not-found" {
		t.Fatalf("--side server with no servers: %+v", env.Error)
	}

	if err := os.RemoveAll(filepath.Join(gameDir, instance.Dir, "backups")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, instance.Dir, "backups"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env = savesRunOf[backupResult](t, h, 1, "backup", "--all")
	if env.OK || env.Error == nil || env.Error.Code != "backup-failed" || len(env.Data) != 3 {
		t.Fatalf("a failed row fails the run once the rest are done: %+v", env)
	}
	if runs := runsByKey(t, env.Data); runs["friends"].OK || runs["friends"].Error == nil || !runs["default"].OK {
		t.Fatalf("only friends failed: %+v", env.Data)
	}
}

func TestRestoreAndSavesAllRunOverTheSameRows(t *testing.T) {
	h := newHarness(t)
	fourRows(t, h)
	h.mustRun(t, "backup", "--all")
	h.mustRun(t, "backup", "--all")

	views := runsByKey(t, savesRunOf[savesView](t, h, 0, "saves", "--all").Data)
	if len(views) != 3 || len(views["default"].Result.Backups) != 2 || len(views["friends"].Result.Backups) != 2 || len(views["empty"].Result.Backups) != 0 {
		t.Fatalf("saves --all: %+v", views)
	}
	if stdout := h.mustRun(t, "saves", "--all"); !strings.Contains(stdout, "shulker restore <n> -i friends") || !strings.Contains(stdout, "shulker restore <n> --group default") {
		t.Fatalf("each nudge names its own target: %s", stdout)
	} else if !strings.Contains(stdout, "default save group (2 instances)\n  • survival") || !strings.Contains(stdout, "Friends (Prism Launcher)\n  • mine") || !strings.HasSuffix(stdout, "i No worlds or backups: Empty\n") || strings.Contains(stdout, "Backups\n") {
		t.Fatalf("one section per target with worlds or backups, the rest on one line: %s", stdout)
	}

	restored := runsByKey(t, savesRunOf[restoreResult](t, h, 0, "restore", "--all").Data)
	if len(restored) != 3 || restored["default"].Result == nil || restored["friends"].Result == nil {
		t.Fatalf("restore --all: %+v", restored)
	}
	if e := restored["empty"]; !e.OK || e.Result != nil || !strings.Contains(e.Skipped, "has no backups yet") {
		t.Fatalf("a target with no backups is reported, not failed: %+v", e)
	}
	for _, args := range [][]string{
		{"restore", "2", "--all"},
		{"restore", "--all", "--backup", "x"},
		{"restore", "--all", "--world", "survival"},
		{"restore", "--all", "--as", "old"},
		{"backup", "--all", "--world", "survival"},
		{"saves", "--launcher", "prism"},
		{"saves", "prune", "--all", "--group", "default", "--keep", "1"},
	} {
		if env := h.runSetting(t, 2, args...); env.Error == nil || env.Error.Code != "usage" {
			t.Fatalf("%v: %+v", args, env.Error)
		}
	}

	pruned := runsByKey(t, savesRunOf[savesPruned](t, h, 0, "saves", "prune", "--all", "--keep", "1").Data)
	if len(pruned) != 3 || pruned["default"].Result.Kept != 1 || len(pruned["friends"].Result.Pruned) != 2 {
		t.Fatalf("saves prune --all: %+v", pruned)
	}
}
