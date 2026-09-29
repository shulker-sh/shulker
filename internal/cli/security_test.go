package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

func TestSecurityExplainsEveryProtection(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "security")
	if !strings.HasPrefix(stdout, "  Mods run with everything your account can reach") {
		t.Fatalf("opens with the stance: %q", stdout)
	}
	for _, p := range security.Protections() {
		if !strings.Contains(stdout, "  • "+p.Summary+"\n") {
			t.Fatalf("missing %s in %q", p.ID, stdout)
		}
	}
	if strings.Contains(stdout, "Settings") {
		t.Fatalf("no settings table while nothing is configurable: %q", stdout)
	}

	var env struct {
		Data securityInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "security", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Stance != security.Stance || len(env.Data.Protections) != len(security.Protections()) {
		t.Fatalf("json: %+v", env.Data)
	}
	for _, p := range env.Data.Protections {
		if p.ID == "" || p.Summary == "" || !p.On {
			t.Fatalf("json row: %+v", p)
		}
	}
}

func TestSecurityTablesItsSettings(t *testing.T) {
	var buf bytes.Buffer
	info := securityInfo{Stance: "Stance.", Protections: []security.Protection{
		{ID: "always", On: true, Summary: "Always does this."},
		{ID: "age", On: true, Summary: "Holds back new versions.", Setting: "security.minReleaseAge", Value: "7 days", Changes: "Skips versions newer than this"},
	}}
	printSecurity(&out.Lines{W: &buf}, info)
	got := buf.String()
	if !strings.Contains(got, "  • Always does this.\n") || strings.Contains(got, "Holds back new versions.") {
		t.Fatalf("a configurable protection shows in the table, not the list: %q", got)
	}
	if !strings.Contains(got, "  Settings\n") || !strings.Contains(got, "security.minReleaseAge  7 days  Skips versions newer than this") {
		t.Fatalf("settings table: %q", got)
	}
}

func TestAChangedCacheObjectWarnsWithThePointer(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	c := &cache.Cache{Dir: h.cache}
	if err := os.WriteFile(c.Object(h.readLock(t).Mods["sodium"].Sha512), []byte("infected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(h.dir, "build")); err != nil {
		t.Fatal(err)
	}
	_, _, stderr := h.run(t, "build")
	if !strings.Contains(stderr, "held a changed copy of") || !strings.Contains(stderr, "Read what shulker checks and why:\n    $ shulker security\n") {
		t.Fatalf("stderr: %q", stderr)
	}
	if err := os.WriteFile(c.Object(h.readLock(t).Mods["sodium"].Sha512), []byte("infected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(h.dir, "build")); err != nil {
		t.Fatal(err)
	}
	env := h.runEnvelope(t, 0, "build")
	if len(env.SecurityWarnings) != 1 || env.SecurityWarnings[0].Protection != "cache-hash" || !slices.Contains(env.Warnings, env.SecurityWarnings[0].Message) {
		t.Fatalf("security warnings: %+v, warnings: %q", env.SecurityWarnings, env.Warnings)
	}
}
