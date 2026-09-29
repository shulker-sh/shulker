package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/audit"
)

func auditReport(t *testing.T, stdout string) (bool, audit.Report) {
	t.Helper()
	var env struct {
		OK   bool         `json:"ok"`
		Data audit.Report `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	return env.OK, env.Data
}

func TestAuditListsWhatDeservesALookAndFailsOnlyOnProvenance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "build")
	if stdout := h.mustRun(t, "audit"); !strings.Contains(stdout, "No problems found") {
		t.Fatalf("clean project: %s", stdout)
	}
	if _, rep := auditReport(t, h.mustRun(t, "audit", "--json", "--", "sodium")); len(rep.Keys) != 1 || rep.Keys[0] != "sodium" {
		t.Fatalf("a key after -- narrows the audit: %+v", rep)
	}

	jar := filepath.Join(h.dir, "build", "client", "mods", h.jars["sodium"].filename)
	f, err := os.OpenFile(jar, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("tampered")
	f.Close()
	code, stdout, _ := h.run(t, "audit", "--json")
	ok, rep := auditReport(t, stdout)
	if code != 0 || !ok || len(rep.Installed) != 1 || rep.Installed[0].Key != "sodium" || rep.Installed[0].Problem != audit.Changed {
		t.Fatalf("a changed jar is listed without failing: exit %d %s", code, stdout)
	}

	h.sendSodiumElsewhere(t)
	code, stdout, stderr := h.run(t, "audit")
	if code == 0 || !strings.Contains(stdout, "sodium: locked from Modrinth, downloads from evil.example") || !strings.Contains(stdout, "$ shulker lock sodium") || !strings.Contains(stderr, "1 entry downloads from outside its provider") {
		t.Fatalf("provenance fails the audit: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	code, stdout, _ = h.run(t, "audit", "--json")
	ok, rep = auditReport(t, stdout)
	if code == 0 || ok || failureCode(t, stdout).Code != "audit-failed" || len(rep.Provenance) != 1 || rep.Provenance[0].Host != "evil.example" {
		t.Fatalf("--json carries the report on failure: exit %d %s", code, stdout)
	}

	if code, stdout, _ = h.run(t, "audit", "nope", "--json"); code == 0 || failureCode(t, stdout).Code != "mod-not-found" {
		t.Fatalf("an unknown key is refused: exit %d %s", code, stdout)
	}
}

func TestAuditOfALinkedInstanceReadsTheLockItRunsOn(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	h.mustRun(t, "hook", "pre-launch", "-C", gameDir)
	if err := os.WriteFile(filepath.Join(gameDir, "mods", "stray.jar"), []byte("stray"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.dir = ""
	code, stdout, _ := h.run(t, "audit", "-i", "Friends", "--json")
	ok, rep := auditReport(t, stdout)
	if code != 0 || !ok || len(rep.Installed) != 1 || rep.Installed[0].Path != "mods/stray.jar" || rep.Installed[0].Problem != audit.Unlisted || rep.Installed[0].Dir != gameDir {
		t.Fatalf("the instance's mods/ is checked: exit %d %s", code, stdout)
	}
}
