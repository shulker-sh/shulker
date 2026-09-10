package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmast/shulker/internal/build"
)

func TestSyncIntoDirectory(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	into := filepath.Join(t.TempDir(), "instance", "minecraft")
	stdout := h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "client: ") || !strings.Contains(stdout, "into "+into) {
		t.Fatalf("sync output: %s", stdout)
	}
	for _, rel := range []string{"mods/" + h.jars["sodium"].filename, "mods/" + h.jars["fabric-api"].filename, "options.txt", build.StateFile} {
		if _, err := os.Stat(filepath.Join(into, rel)); err != nil {
			t.Fatalf("expected %s in the sync directory: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("sync must not touch the project build directory: %v", err)
	}
	link, err := os.Readlink(filepath.Join(into, "saves"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Rel(into, filepath.Join(h.dir, build.DataDir, "client", "saves"))
	if link != want {
		t.Fatalf("saves link = %q, want %q", link, want)
	}

	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "fetched 0 file(s)") || !strings.Contains(stdout, "0 written") {
		t.Fatalf("second sync output: %s", stdout)
	}

	var env struct {
		Data syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", h.dir, "--into", into, "--target", "client", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; res.Target != "client" || res.Dir != into || res.Build == nil || res.Build.Dir != into {
		t.Fatalf("json result: %+v", res)
	}
}

func TestSyncErrors(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "sync", t.TempDir(), "--json")
	if code == 0 || failureCode(t, stdout).Code != "project-not-found" {
		t.Fatalf("missing project: exit %d %s", code, stdout)
	}

	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	code, stdout, _ = h.run(t, "sync", h.dir, "--target", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "target-not-found" || strings.Join(e.Candidates, ",") != "client" {
		t.Fatalf("unknown target: exit %d %s", code, stdout)
	}
}
