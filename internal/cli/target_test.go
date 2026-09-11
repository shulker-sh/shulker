package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shulker-sh/shulker/internal/manifest"
	"github.com/shulker-sh/shulker/internal/out"
)

func TestTargetAddRemoveList(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	lockBefore, err := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}

	if stdout := h.mustRun(t, "target", "add", "server"); !strings.Contains(stdout, "Next: shulker build server") {
		t.Fatalf("add output: %s", stdout)
	}
	h.mustRun(t, "target", "add", "fancy", "--side", "client", "--build", "out/fancy",
		"--overrides", "overrides", "--overrides", "fancy-overrides",
		"--feature", "shaders", "--feature", "zoom",
		"--name", "Fancy Client", "--var", "motd=Hi, there", "--var", "level=3", "--note", "for screenshots")

	m := h.readManifest(t)
	if got, want := m.Targets["server"], (manifest.Target{Side: "server", Build: "build/server", Overrides: []string{"overrides"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("server target = %+v, want %+v", got, want)
	}
	want := manifest.Target{
		Name:      "Fancy Client",
		Side:      "client",
		Build:     "out/fancy",
		Overrides: []string{"overrides", "fancy-overrides"},
		Features:  []string{"shaders", "zoom"},
		Variables: map[string]string{"motd": "Hi, there", "level": "3"},
		Note:      "for screenshots",
	}
	if got := m.Targets["fancy"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("fancy target = %+v, want %+v", got, want)
	}
	lockAfter, err := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(lockAfter) != string(lockBefore) {
		t.Fatal("target add rewrote the lock")
	}

	var env struct {
		OK        bool          `json:"ok"`
		LockStale bool          `json:"lockStale"`
		Data      []targetEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "target", "ls", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range env.Data {
		names = append(names, e.ID)
	}
	if !env.OK || env.LockStale || strings.Join(names, ",") != "client,fancy,server" || env.Data[1].Build != "out/fancy" {
		t.Fatalf("list: %+v", env)
	}
	if stdout := h.mustRun(t, "target", "list"); !strings.Contains(stdout, `fancy client out/fancy overrides=overrides,fancy-overrides features=shaders,zoom "Fancy Client"`) {
		t.Fatalf("list output: %s", stdout)
	}

	if err := os.MkdirAll(filepath.Join(h.dir, "build", "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "target", "rm", "server")
	h.mustRun(t, "target", "remove", "fancy")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "server")); err != nil {
		t.Fatalf("build dir removed: %v", err)
	}
	if m := h.readManifest(t); len(m.Targets) != 1 {
		t.Fatalf("targets after remove: %v", m.Targets)
	}
}

func TestTargetErrors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	for _, tc := range []struct {
		args       []string
		code       string
		candidates []string
	}{
		{[]string{"add", "client"}, "target-exists", nil},
		{[]string{"add", "modded"}, "usage", nil},
		{[]string{"add", "modded", "--side", "both"}, "usage", nil},
		{[]string{"add", "Modded", "--side", "client"}, "usage", nil},
		{[]string{"add", "modded", "--side", "client", "--var", "novalue"}, "usage", nil},
		{[]string{"remove", "nope"}, "target-not-found", []string{"client"}},
		{[]string{"remove", "client"}, "last-target", nil},
	} {
		code, stdout, _ := h.run(t, append(append([]string{"target"}, tc.args...), "--json")...)
		var e *out.Error
		if code != 0 {
			e = failureCode(t, stdout)
		}
		if e == nil || e.Code != tc.code || !reflect.DeepEqual(e.Candidates, tc.candidates) {
			t.Fatalf("%v: exit %d, %s", tc.args, code, stdout)
		}
	}
	if m := h.readManifest(t); len(m.Targets) != 1 {
		t.Fatalf("a failed command changed targets: %v", m.Targets)
	}
}

func TestTargetWithoutBuildDir(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.editManifest(t, func(m map[string]any) {
		delete(m["targets"].(map[string]any)["client"].(map[string]any), "build")
	})
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", ".shulker-state.json")); err != nil {
		t.Fatalf("client not built into build/client: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, ".shulker-state.json")); err == nil {
		t.Fatal("client built into the project directory")
	}
	if stdout := h.mustRun(t, "target", "list"); !strings.HasPrefix(stdout, "client client build/client ") {
		t.Fatalf("list output: %s", stdout)
	}
	h.mustRun(t, "target", "add", "server")
	data, err := os.ReadFile(filepath.Join(h.dir, "shulker.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"build"`) != 1 {
		t.Fatalf("saved manifest should keep client's build omitted:\n%s", data)
	}
}
