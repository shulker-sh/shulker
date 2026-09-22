package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/resolve"
)

type matchResult struct {
	Locked []resolve.LockedFile `json:"locked"`
	Moved  []string             `json:"moved"`
	Kept   []string             `json:"kept"`
}

func TestMatchLocksOverrideFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	sodium, jei, iris, nodist, fresh := h.jars["sodium"], h.jars["jei"], h.jars["irisshaders"], h.jars["nodist"], h.jars["fresh-animations"]
	files := map[string][]byte{
		"overrides/mods/" + sodium.filename:                   sodium.data,
		"overrides/mods/" + jei.filename:                      jei.data,
		"client-overrides/mods/" + iris.filename:              iris.data,
		"overrides/mods/" + nodist.filename:                   nodist.data,
		"overrides/mods/unknown-1.0.jar":                      []byte("not on any provider"),
		"client-overrides/resourcepacks/Fresh Animations.zip": fresh.data,
		"overrides/config/client.txt":                         []byte("setting=1"),
	}
	for rel, data := range files {
		writeFile(t, filepath.Join(h.dir, rel), string(data))
	}
	match := func(t *testing.T, args ...string) (matchResult, []string) {
		t.Helper()
		var env struct {
			Warnings []string    `json:"warnings"`
			Data     matchResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, append(append([]string{"match"}, args...), "--json")...)), &env); err != nil {
			t.Fatal(err)
		}
		return env.Data, env.Warnings
	}
	wantLocked := []resolve.LockedFile{
		{ID: "fresh-animations", Type: "resourcepack", Provider: "modrinth"},
		{ID: "iris", Type: "mod", Provider: "curseforge"},
		{ID: "jei", Type: "mod", Provider: "curseforge"},
		{ID: "sodium", Type: "mod", Provider: "modrinth"},
	}
	wantKept := "overrides/mods/" + nodist.filename + ",overrides/mods/unknown-1.0.jar"

	res, _ := match(t, "--dry-run")
	if !slices.Equal(res.Locked, wantLocked) || strings.Join(res.Kept, ",") != wantKept || len(res.Moved) != 4 {
		t.Fatalf("dry run: %+v", res)
	}
	if got := h.mustRun(t, "match", "--dry-run"); !strings.Contains(got, "3 mods (2 CurseForge, 1 Modrinth), 1 resource pack (Modrinth) locked, 2 files kept as overrides") {
		t.Fatalf("dry run text:\n%s", got)
	}
	if m, _ := readProject(t, h.dir); len(m.Requires) != 0 {
		t.Fatalf("dry run wrote requires: %+v", m.Requires)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "overrides/mods", sodium.filename)); err != nil {
		t.Fatalf("dry run removed an override: %v", err)
	}

	res, warnings := match(t)
	if !slices.Equal(res.Locked, wantLocked) || strings.Join(res.Kept, ",") != wantKept {
		t.Fatalf("match: %+v", res)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], nodist.filename) || !strings.Contains(warnings[0], "third-party downloads") {
		t.Fatalf("warnings: %v", warnings)
	}
	m, l := readProject(t, h.dir)
	if l.Mods["sodium"].Provider != "modrinth" || l.Mods["jei"].Provider != "curseforge" || l.Mods["iris"].Side != "client" || l.ResourcePacks["fresh-animations"].Filename != "Fresh Animations.zip" {
		t.Fatalf("lock: %+v %+v", l.Mods, l.ResourcePacks)
	}
	if _, ok := m.Requires["jei"]; !ok {
		t.Fatalf("requires: %+v", m.Requires)
	}
	for _, rel := range res.Moved {
		if _, err := os.Stat(filepath.Join(h.dir, rel)); !os.IsNotExist(err) {
			t.Fatalf("locked file %s kept as an override: %v", rel, err)
		}
	}
	for _, rel := range append(res.Kept, "overrides/config/client.txt") {
		if _, err := os.Stat(filepath.Join(h.dir, rel)); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, filepath.Join(h.dir, "overrides/mods", sodium.filename), string(sodium.data))
	res, warnings = match(t, filepath.Join("overrides", "mods", sodium.filename))
	if len(res.Locked) != 0 || strings.Join(res.Kept, ",") != "overrides/mods/"+sodium.filename {
		t.Fatalf("match of a mod requires holds: %+v", res)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "requires already has sodium") {
		t.Fatalf("warnings: %v", warnings)
	}

	if code, _, stderr := h.run(t, "match", "overrides/config/client.txt"); code == 0 || !strings.Contains(stderr, "override-path") {
		t.Fatalf("match of a config file: exit %d, %s", code, stderr)
	}
	if code, _, stderr := h.run(t, "match", "overrides/mods/gone.jar"); code == 0 || !strings.Contains(stderr, "file-not-found") {
		t.Fatalf("match of a missing jar: exit %d, %s", code, stderr)
	}
}
