package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
)

// shulkerInstances is the instances root the harness links into, which TestMain and this test point
// at scratch directories so nothing lands in the data directory of the machine running it.
func shulkerInstances(t *testing.T, h *harness) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "instances")
	h.mustRun(t, "config", "set", "instances", root)
	return root
}

func TestLinkShulker(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	stdout := h.mustRun(t, "link", "shulker")
	gameDir := filepath.Join(root, "pack")
	if !strings.Contains(stdout, "created instance pack") || !strings.Contains(stdout, "follows pack from "+h.dir) {
		t.Fatalf("link output: %s", stdout)
	}

	m := instanceManifest(t, gameDir)
	client, _ := m["client"].(map[string]any)
	if m["name"] != "pack" || client["build"] != "." {
		t.Fatalf("a shulker instance is a project built in place: %v", m)
	}
	key, entry := onlyModpack(t, m)
	if key != "pack" || entry["source"] != h.dir {
		t.Fatalf("modpack entry %s: %v", key, entry)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("a link builds the instance before it returns: %v", err)
	}

	instances := readInstances(t, h)
	want := config.Instance{ID: "pack", Name: "pack", Launcher: "shulker", Dir: gameDir, Source: h.dir}
	if len(instances) != 1 || withoutStamp(t, instances[0]) != want {
		t.Fatalf("registry = %+v, want %+v", instances, want)
	}

	// Shulker is the launcher here, so it runs the hooks itself: nothing writes a slot command and
	// no script is generated for another program to run.
	for _, kind := range []launcher.HookKind{launcher.HookPreLaunch, launcher.HookPostExit} {
		if _, err := os.Stat(filepath.Join(gameDir, instance.Dir, string(kind))); !os.IsNotExist(err) {
			t.Fatalf("a shulker instance needs no %s script: %v", kind, err)
		}
	}
	if _, fills := launcher.SlotOf("shulker"); fills {
		t.Fatal("shulker fills no launcher slot")
	}

	if out := h.mustRun(t, "instances"); !strings.Contains(out, "Shulker") || !strings.Contains(out, "pack") {
		t.Fatalf("instances: %s", out)
	}
	// -i reaches it, and the commands that need a project in place all find one. The harness always
	// passes -C, which -i refuses to sit beside, so the scope flag is the only one for this block.
	project := h.dir
	h.dir = ""
	h.mustRun(t, "-i", "pack", "sync")
	h.mustRun(t, "-i", "pack", "update")
	if out := h.mustRun(t, "-i", "pack", "history"); out == "" {
		t.Fatal("history should read the instance's own log")
	}
	h.dir = project

	if out := h.mustRun(t, "unlink", "pack"); !strings.Contains(out, "the instance directory and its worlds stay") {
		t.Fatalf("unlink: %s", out)
	}
	if len(readInstances(t, h)) != 0 {
		t.Fatalf("unlink drops the row: %+v", readInstances(t, h))
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("unlink leaves the instance where it is: %v", err)
	}
}

func TestLinkShulkerNicknames(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")

	h.mustRun(t, "link", "shulker", "--as", "smp")
	if _, err := os.Stat(filepath.Join(root, "smp", "shulker.json")); err != nil {
		t.Fatalf("--as names the folder under the instances root: %v", err)
	}
	if in := readInstances(t, h); len(in) != 1 || in[0].ID != "smp" {
		t.Fatalf("--as names the row too: %+v", in)
	}

	// A second source under the same nickname is a different instance asking for one folder.
	other := t.TempDir()
	h.mustRun(t, "-C", other, "init", "--yes", "--loader", "fabric", "--name", "pack")
	code, _, stderr := h.run(t, "-C", other, "link", "shulker", "--as", "smp")
	if code == 0 || !strings.Contains(stderr, "already follows") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	h.mustRun(t, "-C", other, "link", "shulker")
	if _, err := os.Stat(filepath.Join(root, "pack", "shulker.json")); err != nil {
		t.Fatalf("without --as the pack's own name is free: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "smp", "shulker.json")); err != nil {
		t.Fatalf("the refused link must leave the first instance alone: %v", err)
	}
}

func TestInstancesRepairFindsShulkersOwnInstances(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "link", "shulker", "--as", "smp")

	for _, args := range [][]string{
		{"instances", "repair", "--launcher", "shulker"},
		{"instances", "repair", "--launcher", "shulker", "--launcher-dir", root},
	} {
		if err := config.WriteInstances(registryPath(h), nil); err != nil {
			t.Fatal(err)
		}
		if stdout := h.mustRun(t, args...); !strings.Contains(stdout, "registered smp") {
			t.Fatalf("%v scans the instances root: %s", args, stdout)
		}
		in := readInstances(t, h)
		if len(in) != 1 || in[0].ID != "smp" || in[0].Launcher != "shulker" || in[0].Dir != filepath.Join(root, "smp") || in[0].LauncherDir != "" {
			t.Fatalf("%v rebuilds the row link wrote: %+v", args, in)
		}
	}
}
