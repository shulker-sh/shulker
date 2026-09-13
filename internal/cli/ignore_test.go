package cli

import (
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestIgnoreCommandWritesAndDropsEntries(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"depends":{"fabricloader":">=0.17","fabric-api":">=2.0.0"}`)
	h.mustRun(t, "init", "--yes")
	if code, _, _ := h.run(t, "add", "sodium"); code == 0 {
		t.Fatal("add should fail validation")
	}

	code, stdout, _ := h.run(t, "ignore", "sodium", "fabric-api", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Message, "--note") {
		t.Fatalf("ignore without --note: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "ignore", "sodium", "fabric-api", "--declared", ">=2.0.0", "--note", "x", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Message, "--rule") {
		t.Fatalf("--declared without --rule: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "ignore", "sodium", "fabric-api", "--note", "x", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-problem" || !strings.Contains(e.Message, "--declared") {
		t.Fatalf("short form with nothing in the lock: exit %d %s", code, stdout)
	}

	stdout = h.mustRun(t, "ignore", "sodium", "fabric-api", "--rule", "depends", "--declared", ">=2.0.0", "--note", "works on 1.x")
	if !strings.Contains(stdout, "ignored sodium on fabric-api") || !strings.Contains(stdout, "depends >=2.0.0") {
		t.Fatalf("ignore output: %s", stdout)
	}
	var m struct {
		Ignore []struct {
			Rule, Mod, On, Declared, Note string
		} `json:"ignore"`
	}
	m.Ignore = nil
	h.readJSON(t, "shulker.json", &m)
	if len(m.Ignore) != 1 || m.Ignore[0].Declared != ">=2.0.0" || m.Ignore[0].Note != "works on 1.x" {
		t.Fatalf("manifest ignore: %+v", m.Ignore)
	}
	h.mustRun(t, "add", "sodium")

	code, stdout, _ = h.run(t, "ignore", "sodium", "fabric-api", "--note", "again", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "already-ignored" {
		t.Fatalf("second ignore without --force: exit %d %s", code, stdout)
	}
	stdout = h.mustRun(t, "ignore", "sodium", "fabric-api", "--force", "--note", "still fine")
	if !strings.Contains(stdout, "replaced ignore for sodium on fabric-api") {
		t.Fatalf("forced ignore output: %s", stdout)
	}
	m.Ignore = nil
	h.readJSON(t, "shulker.json", &m)
	if len(m.Ignore) != 1 || m.Ignore[0].Declared != ">=2.0.0" || m.Ignore[0].Note != "still fine" {
		t.Fatalf("manifest after --force: %+v", m.Ignore)
	}

	stdout = h.mustRun(t, "unignore", "sodium", "fabric-api")
	if !strings.Contains(stdout, "unignored sodium on fabric-api") {
		t.Fatalf("unignore output: %s", stdout)
	}
	m.Ignore = nil
	h.readJSON(t, "shulker.json", &m)
	if len(m.Ignore) != 0 {
		t.Fatalf("manifest after unignore: %+v", m.Ignore)
	}
	code, stdout, _ = h.run(t, "unignore", "sodium", "fabric-api", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "not-ignored" {
		t.Fatalf("unignore twice: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "install", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "validation-failed" {
		t.Fatalf("install after unignore: exit %d %s", code, stdout)
	}

	code, stdout, _ = h.run(t, "ignore", "sodium", "iris", "--note", "x", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-problem" || strings.Join(e.Candidates, ",") != "fabric-api" {
		t.Fatalf("wrong on for a mod with a problem: exit %d %s", code, stdout)
	}
	h.mustRun(t, "ignore", "sodium", "fabric-api", "--note", "inferred from the lock")
	m.Ignore = nil
	h.readJSON(t, "shulker.json", &m)
	if len(m.Ignore) != 1 || m.Ignore[0].Rule != "depends" || m.Ignore[0].Declared != ">=2.0.0" {
		t.Fatalf("short form should infer from the locked problem: %+v", m.Ignore)
	}
	h.mustRun(t, "install")
}
