package cli

import (
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestAddAsKeysAMod(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium", "--as", "speed")

	var m struct {
		Requires map[string]struct {
			Project string `json:"project"`
		} `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if _, keyed := m.Requires["speed"]; !keyed {
		t.Fatalf("requires should be keyed by --as: %v", m.Requires)
	}
	if got := m.Requires["speed"].Project; got != "AANobbMI" {
		t.Fatalf("the entry should record its project, got %q", got)
	}

	var l struct {
		Mods map[string]struct {
			ModID         string   `json:"modId"`
			VersionNumber string   `json:"versionNumber"`
			RequiredBy    []string `json:"requiredBy"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if got := l.Mods["speed"].ModID; got != "sodium" {
		t.Fatalf("the lock should record the jar id, got %q", got)
	}
	if _, keyed := l.Mods["sodium"]; keyed {
		t.Fatalf("the jar id should not key a lock entry: %v", l.Mods)
	}
	if by := l.Mods["fabric-api"].RequiredBy; len(by) != 1 || by[0] != "speed" {
		t.Fatalf("requiredBy should name the requires key: %v", by)
	}

	h.newer = true
	h.mustRun(t, "update")
	h.readJSON(t, "shulker.lock", &l)
	if got := l.Mods["speed"]; got.ModID != "sodium" || got.VersionNumber != "1.1.0+mc26.2" {
		t.Fatalf("update should keep the key and refresh the version: %+v", got)
	}
}

func TestAddRefusesATakenKey(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "add", "fabric-api", "--as", "sodium", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "requires-taken" {
		t.Fatalf("a taken key should be refused: code=%d %s", code, stdout)
	}
}

func TestAddRefusesOneJarIDUnderTwoKeys(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium", "--as", "speed")

	code, stdout, _ := h.run(t, "add", "sodium", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "requires-taken" {
		t.Fatalf("one jar id under two keys should be refused: code=%d %s", code, stdout)
	}
}

func TestAddRefusesAnInvalidAsKey(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")

	code, stdout, _ := h.run(t, "add", "sodium", "--as", "Speed Mod", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" {
		t.Fatalf("an invalid --as should be a usage error: code=%d %s", code, stdout)
	}
}
