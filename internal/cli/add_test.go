package cli

import (
	"strings"
	"testing"
)

func TestAddDoesNotNudgeAnInstallOfWhatItFetched(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	if stdout := h.mustRun(t, "add", "sodium"); !strings.Contains(stdout, "+ sodium") || strings.Contains(stdout, "shulker install") {
		t.Fatalf("add: %s", stdout)
	}
}

func TestAddKeepsTheModsThatValidate(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"depends":{"mixinextras":">=0.5"}`)
	h.mustRun(t, "create", "--loader", "fabric")

	code, stdout, stderr := h.run(t, "add", "sodium", "fabric-api")
	if code == 0 || !strings.Contains(stdout, "+ fabric-api") || strings.Contains(stdout, "+ sodium") || !strings.Contains(stderr, "requires mixinextras") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	m := h.readManifest(t)
	if _, ok := m.Requires["fabric-api"]; !ok {
		t.Fatalf("fabric-api validated and should be added: %+v", m.Requires)
	}
	if _, ok := m.Requires["sodium"]; ok {
		t.Fatalf("sodium failed validation and should not be added: %+v", m.Requires)
	}
	if _, ok := h.readLock(t).Mods["sodium"]; ok {
		t.Fatal("sodium should not be locked")
	}

	code, stdout, _ = h.run(t, "add", "sodium", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "validation-failed" {
		t.Fatalf("adding only a failing mod fails as before: exit %d %s", code, stdout)
	}
}

func TestAddSuggestsOnlyForWhatItAdded(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"suggests":{"modmenu":"*"}`)
	h.mustRun(t, "create", "--loader", "fabric")
	if stdout := h.mustRun(t, "add", "sodium"); !strings.Contains(stdout, "suggests modmenu") {
		t.Fatalf("add should list what the mod it added suggests: %s", stdout)
	}
	if stdout := h.mustRun(t, "resourcepack", "add", "fresh-animations"); strings.Contains(stdout, "modmenu") {
		t.Fatalf("a later add lists an earlier mod's suggestion: %s", stdout)
	}
}

func TestAddOfAModAlreadyInThePackSaysSo(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	if stdout := h.mustRun(t, "add", "sodium"); !strings.Contains(stdout, "i sodium is already in the pack\n") {
		t.Errorf("add again: %s", stdout)
	}
}
