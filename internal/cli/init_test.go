package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestInitChecksTheTarget(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "create", "--loader", "fabric", "--side", "weird", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || len(e.Candidates) != 2 {
		t.Fatalf("unknown side: code=%d %s", code, stdout)
	}
}

func TestFailedInitLeavesNothingBehind(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run(t, "create", "--loader", "fabric", "--name", "..."); code == 0 {
		t.Fatal("an invalid name must fail")
	}
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("a failed init left %s behind", e.Name())
	}
}

// create never asks, and init off a terminal has nobody to ask, so both create the same project
// and print the same lines.
func TestInitWithoutAnswersTakesTheDefaults(t *testing.T) {
	h := newHarness(t)
	var first, firstManifest string
	for _, args := range [][]string{{"create"}, {"init", "--no-input"}, {"init"}} {
		dir := t.TempDir()
		stdout := h.mustRun(t, append(args, "--name", "pack", "-C", dir)...)
		data, err := os.ReadFile(filepath.Join(dir, "shulker.json"))
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first, firstManifest = stdout, string(data)
			if !strings.Contains(stdout, "Created shulker.json") {
				t.Fatalf("%v: %s", args, stdout)
			}
			if gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(gi) != "/build/\n/data/\n/downloads/\n/shulker.local.json\n/.shulker/\n" {
				t.Fatalf(".gitignore: %q", gi)
			}
			continue
		}
		if stdout != first {
			t.Fatalf("%v prints\n%s\nwant\n%s", args, stdout, first)
		}
		if string(data) != firstManifest {
			t.Fatalf("%v writes\n%s\nwant\n%s", args, data, firstManifest)
		}
	}
}

func TestNoInputIsGlobal(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--no-input", "--name", "pack", "--loader", "fabric")
	if out := h.mustRun(t, "list", "--no-input", "--json"); !strings.Contains(out, `"ok": true`) {
		t.Fatalf("list --no-input: %s", out)
	}
	code, stdout, _ := h.run(t, "init", "--no-input", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "manifest-exists" {
		t.Fatalf("a second init: exit %d, %s", code, stdout)
	}
}

func TestCreateNeverAsks(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr, s := h.runAnswering(t, nil, "create", "--name", "pack")
	if code != 0 || len(s.asked) != 0 || !strings.Contains(stdout, "Created shulker.json") {
		t.Fatalf("create asked %v: exit %d\n%s%s", s.asked, code, stdout, stderr)
	}
	code, _, stderr = h.run(t, "init", "--yes", "-C", t.TempDir())
	if code == 0 || !strings.Contains(stderr, "Unknown flag") {
		t.Fatalf("init --yes: exit %d, %s", code, stderr)
	}
}

func TestLoaderFlagsStandInForLoader(t *testing.T) {
	h := newHarness(t)
	for _, cmd := range []string{"create", "init"} {
		for _, name := range loader.Names() {
			dir := t.TempDir()
			res := initJSON(t, h.mustRun(t, cmd, "--"+name, "--name", "pack", "--json", "-C", dir))
			if res.Loader != name || res.Version == "" {
				t.Fatalf("%s --%s: %+v", cmd, name, res)
			}
		}
	}
	res := initJSON(t, h.mustRun(t, "create", "--fabric", "--loader", "fabric", "--loader-version", "0.17.3", "--json", "-C", t.TempDir()))
	if res.Loader != "fabric" || res.Version != "0.17.3" {
		t.Fatalf("--fabric with --loader fabric: %+v", res)
	}
	for _, args := range [][]string{{"--fabric", "--quilt"}, {"--fabric", "--loader", "quilt"}, {"--forge", "--loader", "none"}} {
		code, stdout, _ := h.run(t, append([]string{"create", "--json", "-C", t.TempDir()}, args...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" {
			t.Fatalf("%v: exit %d, %s", args, code, stdout)
		}
	}
}

func TestSideFlagsStandInForSide(t *testing.T) {
	h := newHarness(t)
	for _, cmd := range []string{"create", "init"} {
		for _, side := range manifest.SideNames {
			res := initJSON(t, h.mustRun(t, cmd, "--"+side, "--name", "pack", "--json", "-C", t.TempDir()))
			if res.Side != side {
				t.Fatalf("%s --%s: %+v", cmd, side, res)
			}
		}
	}
	_, _, _, s := h.runAnswering(t, map[string]string{"Start from an existing pack?": ""}, "init", "--server", "--fabric", "--minecraft", "26.2", "--loader-version", "0.17.3", "-C", t.TempDir())
	if len(s.asked) != 1 {
		t.Fatalf("an alias should answer its question, asked %v", s.asked)
	}
	res := initJSON(t, h.mustRun(t, "create", "--server", "--side", "server", "--json", "-C", t.TempDir()))
	if res.Side != "server" {
		t.Fatalf("--server with --side server: %+v", res)
	}
	for _, args := range [][]string{{"--client", "--server"}, {"--server", "--side", "client"}} {
		code, stdout, _ := h.run(t, append([]string{"create", "--json", "-C", t.TempDir()}, args...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" {
			t.Fatalf("%v: exit %d, %s", args, code, stdout)
		}
	}
}

func initJSON(t *testing.T, stdout string) initResult {
	t.Helper()
	var env struct {
		Data initResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	return env.Data
}
