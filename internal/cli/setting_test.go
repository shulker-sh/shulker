package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type settingEnvelope struct {
	OK        bool            `json:"ok"`
	LockStale bool            `json:"lockStale"`
	Warnings  []string        `json:"warnings"`
	Data      json.RawMessage `json:"data"`
	Error     *struct {
		Code       string   `json:"code"`
		Message    string   `json:"message"`
		Candidates []string `json:"candidates"`
		Items      []string `json:"items"`
	} `json:"error"`
}

func (h *harness) runSetting(t *testing.T, wantExit int, args ...string) settingEnvelope {
	t.Helper()
	code, stdout, stderr := h.run(t, append(args, "--json")...)
	if code != wantExit {
		t.Fatalf("%v exited %d, want %d\nstdout: %s\nstderr: %s", args, code, wantExit, stdout, stderr)
	}
	var env settingEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, stdout)
	}
	return env
}

func TestSetGetUnset(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	lockBefore, err := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}

	if stdout := h.mustRun(t, "set", "server.eula", "true"); stdout != "  ~ server.eula (unset) ⟶ true\n" {
		t.Fatalf("set output = %q", stdout)
	}
	h.mustRun(t, "set", "server.properties.rcon.port", "25575")
	h.mustRun(t, "set", "server.properties.motd", "20")
	h.mustRun(t, "set", "variables.port", "25565")
	h.mustRun(t, "set", "variables.fancy", "true")
	h.mustRun(t, "set", "variables.zip", "--literal", `"02134"`)
	h.mustRun(t, "set", "server.jvmArgs", "--literal", `["-XX:+UseZGC"]`)
	h.mustRun(t, "set", "server.players.ops", "Notch")
	h.mustRun(t, "set", "server.players.ops", "069a79f4-44e9-4726-a5be-fca90e38aaf5")
	h.mustRun(t, "set", "server.players.ops", "notch")
	h.mustRun(t, "set", "server.players.whitelist", "Alice")
	h.mustRun(t, "set", "server.players.whitelist", "alice:11111111-1111-4111-8111-111111111111")
	h.mustRun(t, "set", "server.players.whitelist", "Bob:22222222-2222-4222-8222-222222222222")
	for _, value := range []string{
		"Alice:33333333-3333-4333-8333-333333333333",
		"Carol:22222222-2222-4222-8222-222222222222",
		"Alice:22222222-2222-4222-8222-222222222222",
	} {
		if code, _, stderr := h.run(t, "set", "server.players.whitelist", value); code == 0 {
			t.Errorf("set whitelist %s succeeded; stderr %q", value, stderr)
		}
	}
	h.mustRun(t, "set", "loader.note", "kept on save")

	m := h.readManifest(t)
	if m.Server == nil || m.Server.Players == nil || len(m.Server.Players.Ops) != 2 {
		t.Fatalf("server = %+v", m.Server)
	}
	if w := m.Server.Players.Whitelist; len(w) != 2 || w[0].Name != "Alice" || w[0].UUID != "11111111-1111-4111-8111-111111111111" || w[1].Name != "Bob" || w[1].UUID != "22222222-2222-4222-8222-222222222222" {
		t.Errorf("whitelist = %+v", w)
	}
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"server.eula", m.Server.Eula, true},
		{"rcon.port", m.Server.Properties["rcon.port"], json.Number("25575")},
		{"properties.motd", m.Server.Properties["motd"], "20"},
		{"variables.port", m.Variables["port"], json.Number("25565")},
		{"variables.fancy", m.Variables["fancy"], true},
		{"variables.zip", m.Variables["zip"], "02134"},
		{"jvmArgs", strings.Join(m.Server.JVMArgs, " "), "-XX:+UseZGC"},
		{"loader.note", m.Loader.Note, "kept on save"},
		{"op name", m.Server.Players.Ops[0].Name, "Notch"},
		{"op uuid", m.Server.Players.Ops[1].UUID, "069a79f4-44e9-4726-a5be-fca90e38aaf5"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	lockAfter, err := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(lockAfter) != string(lockBefore) {
		t.Fatal("set rewrote the lock")
	}

	if got := h.mustRun(t, "get", "variables.zip"); got != "02134\n" {
		t.Fatalf("get variables.zip = %q", got)
	}
	if got := h.mustRun(t, "get", "variables.port"); got != "25565\n" {
		t.Fatalf("get variables.port = %q", got)
	}
	var props map[string]any
	if err := json.Unmarshal(h.runSetting(t, 0, "get", "server.properties").Data, &props); err != nil || props["rcon.port"] != float64(25575) {
		t.Fatalf("get server.properties = %v (%v)", props, err)
	}
	var whole map[string]any
	if err := json.Unmarshal(h.runSetting(t, 0, "get").Data, &whole); err != nil || whole["name"] != m.Name {
		t.Fatalf("get = %v (%v)", whole, err)
	}
	if env := h.runSetting(t, 1, "get", "server.memory"); env.Error == nil || env.Error.Code != "path-not-set" {
		t.Fatalf("get server.memory error = %+v", env.Error)
	}

	if got := h.mustRun(t, "unset", "server.memory"); got != "  i server.memory was not set\n" {
		t.Fatalf("unset of an unset path = %q", got)
	}
	var change map[string]any
	if err := json.Unmarshal(h.runSetting(t, 0, "unset", "variables.port").Data, &change); err != nil {
		t.Fatal(err)
	}
	if _, hasTo := change["to"]; change["path"] != "variables.port" || change["from"] != float64(25565) || hasTo {
		t.Fatalf("unset data = %v", change)
	}
	if _, ok := h.readManifest(t).Variables["port"]; ok {
		t.Fatal("unset left variables.port in shulker.json")
	}
	if got := h.mustRun(t, "unset", "server.eula"); got != "  ~ server.eula true ⟶ false\n" {
		t.Fatalf("unset server.eula reports %q; the saved file keeps eula false", got)
	}
}

func TestSetRejectsBadPathsAndValues(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	before, err := os.ReadFile(filepath.Join(h.dir, "shulker.json"))
	if err != nil {
		t.Fatal(err)
	}

	env := h.runSetting(t, 1, "set", "sever.eula", "true")
	if env.Error.Code != "path-invalid" || strings.Contains(env.Error.Message, "did you mean") || !slices.Contains(env.Error.Candidates, "server") {
		t.Fatalf("typo error = %+v", env.Error)
	}
	for _, c := range []struct {
		exit     int
		code     string
		contains string
		args     []string
	}{
		{1, "path-invalid", "single value", []string{"set", "name.first", "x"}},
		{1, "path-invalid", "is a list", []string{"set", "server.jvmArgs.first", "x"}},
		{2, "usage", "true or false", []string{"set", "server.eula", "yes"}},
		{2, "usage", "--literal", []string{"set", "server.jvmArgs", "UseZGC"}},
		{2, "usage", "--literal", []string{"set", "variables.x", "--literal", "{nope"}},
		{1, "manifest-invalid", "server.memory: ", []string{"set", "server.memory", "6"}},
		{1, "manifest-invalid", "name", []string{"unset", "name"}},
	} {
		env := h.runSetting(t, c.exit, c.args...)
		if env.Error == nil || env.Error.Code != c.code || !strings.Contains(env.Error.Message, c.contains) {
			t.Errorf("%v error = %+v, want %s containing %q", c.args, env.Error, c.code, c.contains)
		}
	}

	after, err := os.ReadFile(filepath.Join(h.dir, "shulker.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("a failed set changed shulker.json:\n%s", after)
	}
}

func TestSetWarnsOnlyWhenTheLockDiffers(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")

	if env := h.runSetting(t, 0, "set", "providers", "--literal", `["curseforge","modrinth"]`); env.LockStale || len(env.Warnings) != 0 {
		t.Fatalf("reordering providers: lockStale=%v warnings=%v", env.LockStale, env.Warnings)
	}
	env := h.runSetting(t, 0, "set", "requires.sodium.channel", "beta")
	if !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "sodium") || !strings.Contains(env.Warnings[0], "run `shulker lock`") {
		t.Fatalf("channel change: lockStale=%v warnings=%v", env.LockStale, env.Warnings)
	}
}
