package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTakesASidePositionally(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	h.mustRun(t, "install")

	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "Built client") || !strings.Contains(stdout, "Built server") {
		t.Fatalf("no argument builds every declared side: %s", stdout)
	}
	if stdout := h.mustRun(t, "build", "server"); strings.Contains(stdout, "Built client") || !strings.Contains(stdout, "Built server") {
		t.Fatalf("an argument builds that side alone: %s", stdout)
	}

	code, stdout, _ := h.run(t, "build", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || e.Message != `"nope" is not a side` || strings.Join(e.Candidates, ",") != "client,server" {
		t.Fatalf("not a side: exit %d %s", code, stdout)
	}

	h.editManifest(t, func(m map[string]any) { delete(m, "server") })
	code, stdout, stderr := h.run(t, "build", "server", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-side" || e.Message != "shulker.json declares no server" {
		t.Fatalf("undeclared side: exit %d %s", code, stdout)
	}
	if _, _, stderr = h.run(t, "build", "server"); !strings.Contains(stderr, `add "server": {} to shulker.json`) {
		t.Fatalf("no-side hint: %s", stderr)
	}
}

func TestDiffIntoNeedsOneSide(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "diff", "--into", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-side" {
		t.Fatalf("--into across both sides: exit %d %s", code, stdout)
	}
	h.mustRun(t, "diff", "client", "--into", t.TempDir())
}

func TestSyncNeedsOneSideWhenBothAreDeclared(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	twoSided(t, h)

	code, stdout, _ := h.run(t, "sync", h.dir, "--into", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-side" || e.Message != "shulker.json declares both sides" || e.Help != "choose one" || strings.Join(e.Candidates, ",") != "client,server" {
		t.Fatalf("sync across both sides: exit %d %s", code, stdout)
	}
	if _, _, stderr := h.run(t, "sync", h.dir, "--into", t.TempDir()); !strings.Contains(stderr, "--side client") {
		t.Fatalf("the example names --side: %s", stderr)
	}
	code, stdout, _ = h.run(t, "sync", h.dir, "--into", t.TempDir(), "--assume-client", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-side" {
		t.Fatalf("--assume-client is ignored once a client is declared: exit %d %s", code, stdout)
	}
	h.mustRun(t, "sync", h.dir, "--side", "server", "--into", t.TempDir())
}

func TestLauncherCommandsTakeNoSideFlag(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")

	for _, args := range [][]string{
		{"link", "mojang", "--launcher-dir", t.TempDir(), "--side", "client"},
		{"link", "prism", "--launcher-dir", t.TempDir(), "--side", "client"},
		{"export", "curseforge", "--side", "client"},
	} {
		code, stdout, _ := h.run(t, append(args, "--json")...)
		if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || !strings.Contains(e.Message, "--side") {
			t.Fatalf("%v: exit %d %s", args, code, stdout)
		}
	}
}

func TestAssumeClientBuildsAnUndeclaredClient(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	writeOverride(t, h.dir, "overrides/config/shared.txt", "shared\n")
	into := filepath.Join(t.TempDir(), "client")

	code, stdout, _ := h.run(t, "sync", h.dir, "--side", "client", "--into", into, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-side" || e.Message != "shulker.json declares no client" {
		t.Fatalf("no client without the flag: exit %d %s", code, stdout)
	}
	if _, _, stderr := h.run(t, "sync", h.dir, "--side", "client", "--into", into); !strings.Contains(stderr, "pass --assume-client to build one anyway") {
		t.Fatalf("no-side hint names the flag: %s", stderr)
	}
	if _, _, stderr := h.run(t, "build", "client"); strings.Contains(stderr, "--assume-client") || !strings.Contains(stderr, `add "client": {} to shulker.json`) {
		t.Fatalf("a local command keeps the add-the-block hint: %s", stderr)
	}

	var env struct {
		Data     syncResult `json:"data"`
		Warnings []string   `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", h.dir, "--into", into, "--assume-client", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Side != "client" || len(env.Warnings) != 1 || env.Warnings[0] != "shulker.json declares no client; building from shared mods and overrides." {
		t.Fatalf("assumed client: side=%s warnings=%v", env.Data.Side, env.Warnings)
	}
	if data, _ := os.ReadFile(filepath.Join(into, "config", "shared.txt")); string(data) != "shared\n" {
		t.Fatalf("shared overrides are built: %q", data)
	}
	if _, err := os.Stat(filepath.Join(into, "options.txt")); err == nil {
		t.Fatal("an assumed client has no options.txt: there is no client block")
	}
	if f := readIntent(t, into); f.Side != "client" || !f.AssumesClient {
		t.Fatalf("instance file records the assumption: %+v", f)
	}

	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Side != "client" || len(env.Warnings) != 1 {
		t.Fatalf("a later sync of the same directory keeps building: side=%s warnings=%v", env.Data.Side, env.Warnings)
	}

	code, stdout, _ = h.run(t, "sync", h.dir, "--into", t.TempDir(), "--assume-client", "--side", "server", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
		t.Fatalf("--assume-client with --side server: exit %d %s", code, stdout)
	}

	h.editManifest(t, func(m map[string]any) {
		m["client"] = map[string]any{"options": map[string]any{"fov": 1}}
	})
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Warnings) != 0 {
		t.Fatalf("a declared client block wins over the flag: %v", env.Warnings)
	}
	if _, err := os.Stat(filepath.Join(into, "options.txt")); err != nil {
		t.Fatal("the declared client's options are built")
	}

	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", h.dir, "--into", t.TempDir(), "--assume-client", "--side", "server", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Side != "server" || len(env.Warnings) != 0 {
		t.Fatalf("--assume-client is ignored once a client is declared: side=%s warnings=%v", env.Data.Side, env.Warnings)
	}
}

func TestAssumeClientExportsAnUndeclaredClient(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.editManifest(t, func(m map[string]any) { m["version"] = "1.0" })
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "export", "mrpack", "--side", "client", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-side" || e.Message != "shulker.json declares no client" {
		t.Fatalf("export mrpack --side client without a client: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "export", "curseforge", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-side" || e.Message != "shulker.json declares no client" {
		t.Fatalf("export curseforge without a client: exit %d %s", code, stdout)
	}

	var env struct {
		Data     map[string]any `json:"data"`
		Warnings []string       `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "export", "mrpack", "--assume-client", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(env.Data["sides"]) != "[client server]" || len(env.Warnings) != 1 {
		t.Fatalf("assumed client export: sides=%v warnings=%v", env.Data["sides"], env.Warnings)
	}
}

func TestAddWarnsWhenAModShipsOnNoDeclaredSide(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	_, stderr := h.mustRunStderr(t, "add", "sodium")
	if !strings.Contains(stderr, "sodium is client only, so no side of this project ships it; shulker set requires.sodium.side both ships it anyway") {
		t.Fatalf("stderr: %s", stderr)
	}
	if _, stderr := h.mustRunStderr(t, "add", "fabric-api"); strings.Contains(stderr, "no side of this project ships it") {
		t.Fatalf("warned for a mod on every side: %s", stderr)
	}
	if _, stderr := h.mustRunStderr(t, "add", "fresh-animations"); strings.Contains(stderr, "no side of this project ships it") {
		t.Fatalf("warned for a resource pack: %s", stderr)
	}
}

func TestUpdateSkipsTheNoSideWarningForAConditionedMod(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{"shiny": map[string]any{}}
		m["requires"] = map[string]any{"sodium": map[string]any{"feature": "shiny"}}
	})
	if _, stderr := h.mustRunStderr(t, "update"); strings.Contains(stderr, "no side of this project ships it") {
		t.Fatalf("warned for a mod behind a feature: %s", stderr)
	}
}
