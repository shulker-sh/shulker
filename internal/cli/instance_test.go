package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
)

func instanceSettingJSON(t *testing.T, h *harness, args ...string) instanceSetting {
	t.Helper()
	stdout := h.mustRun(t, append(args, "--json")...)
	var env struct {
		Data instanceSetting `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	return env.Data
}

func instanceSettings(t *testing.T, dir string) instance.Settings {
	t.Helper()
	f, err := instance.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return f.Settings
}

func TestInstanceGetShowsTheValueAndTheDefaultBehindIt(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "config", "set", "play.memory", "6G")
	h.dir = gameDir

	if got := instanceSettingJSON(t, h, "instance", "get", "memory"); got.Value != "6G" || got.From != "config" || got.Default != "6G" {
		t.Fatalf("an instance with no memory of its own inherits play.memory: %+v", got)
	}
	if stdout := h.mustRun(t, "instance", "get", "memory"); !strings.HasPrefix(stdout, "6G\n") || !strings.Contains(stdout, "from play.memory") {
		t.Fatalf("get prints the value, then where it came from:\n%s", stdout)
	}

	h.mustRun(t, "instance", "set", "memory", "8G")
	if s := instanceSettings(t, gameDir); s.Memory != "8G" {
		t.Fatalf("set writes the instance file: %+v", s)
	}
	if got := instanceSettingJSON(t, h, "instance", "get", "memory"); got.Value != "8G" || got.From != "instance" || got.Default != "6G" {
		t.Fatalf("the instance's own key wins, and the default is still shown: %+v", got)
	}
	if stdout := h.mustRun(t, "instance", "get", "memory"); !strings.HasPrefix(stdout, "8G\n") || !strings.Contains(stdout, "play.memory is 6G") {
		t.Fatalf("get names the default an unset would return to:\n%s", stdout)
	}

	h.mustRun(t, "instance", "unset", "memory")
	if s := instanceSettings(t, gameDir); s.Memory != "" {
		t.Fatalf("unset removes the key: %+v", s)
	}
	if got := instanceSettingJSON(t, h, "instance", "get", "memory"); got.Value != "6G" || got.From != "config" {
		t.Fatalf("unset returns to the default: %+v", got)
	}
	if stdout := h.mustRun(t, "instance", "unset", "memory"); !strings.Contains(stdout, "memory was not set") {
		t.Fatalf("unsetting an absent key says so:\n%s", stdout)
	}
}

func TestInstanceGetOfAKeySetNowhereFails(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.dir = gameDir

	if env := h.runSetting(t, 1, "instance", "get", "window"); env.Error == nil || env.Error.Code != "path-not-set" {
		t.Fatalf("get window error = %+v", env.Error)
	}
	// A key with a default of its own reports it rather than failing.
	if got := instanceSettingJSON(t, h, "instance", "get", "launchHistory"); got.From != "default" || got.Value != float64(5) {
		t.Fatalf("launchHistory %+v", got)
	}
}

func TestInstanceActsOnANicknameFromElsewhere(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)

	h.mustRun(t, "-i", "pack", "instance", "set", "window", "1280x720")
	h.mustRun(t, "-i", "pack", "instance", "set", "jvmArgs", "--literal", `["-Dmine=1"]`)
	h.mustRun(t, "-i", "pack", "instance", "set", "hooks.preLaunch", "false")

	s := instanceSettings(t, gameDir)
	if s.Window != "1280x720" || len(s.JvmArgs) != 1 || s.JvmArgs[0] != "-Dmine=1" || s.PreLaunch() {
		t.Fatalf("settings %+v", s)
	}
	if got := instanceSettingJSON(t, h, "-i", "pack", "instance", "get", "window"); got.Value != "1280x720" || got.From != "instance" {
		t.Fatalf("get window %+v", got)
	}
}

func TestInstanceRefusesWhatTheSettingsCantHold(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.dir = gameDir

	env := h.runSetting(t, 1, "instance", "set", "colour", "blue")
	if env.Error == nil || env.Error.Code != "path-invalid" || !strings.Contains(strings.Join(env.Error.Candidates, " "), "memory") {
		t.Fatalf("an unknown key names the ones there are: %+v", env.Error)
	}
	for _, bad := range [][]string{{"memory", "lots"}, {"window", "big"}, {"jvmArgs", "-Dx=1"}} {
		if code, stdout, _ := h.run(t, append([]string{"instance", "set", "--json"}, bad...)...); code == 0 {
			t.Fatalf("instance set %v should fail:\n%s", bad, stdout)
		}
	}
	if s := instanceSettings(t, gameDir); s.Memory != "" || s.Window != "" || s.JvmArgs != nil {
		t.Fatalf("a refused value leaves the file alone: %+v", s)
	}
	// Outside an instance there is no instance file to change.
	h.dir = t.TempDir()
	if env := h.runSetting(t, 1, "instance", "get"); env.Error == nil || env.Error.Code != "instance-not-found" {
		t.Fatalf("outside an instance: %+v", env.Error)
	}
}

func TestInstanceSetAccountPinsTheAccountsID(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login")
	h.dir = gameDir

	h.mustRun(t, "instance", "set", "account", "Notch")
	if s := instanceSettings(t, gameDir); s.Account != notchID {
		t.Fatalf("the pin is the account's id, so a rename doesn't break it: %+v", s)
	}
	if env := h.runSetting(t, 1, "instance", "set", "account", "Nobody"); env.Error == nil || env.Error.Code != "account-not-found" {
		t.Fatalf("pinning an account nobody has: %+v", env.Error)
	}
}

func TestInstanceGetWithNoPathPrintsEveryEffectiveSetting(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "config", "set", "play.memory", "6G")
	h.dir = gameDir
	h.mustRun(t, "instance", "set", "window", "800x600")

	stdout := h.mustRun(t, "instance", "get", "--json")
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data["memory"] != "6G" || env.Data["window"] != "800x600" || env.Data["hooks"] == nil {
		t.Fatalf("every setting, the defaults filled in: %v", env.Data)
	}
}

// editorScript is an $EDITOR that replaces the file it is handed with body.
func editorScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	content := filepath.Join(dir, "content.json")
	if err := os.WriteFile(content, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "editor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncp \""+content+"\" \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestInstanceEditOpensTheFileAndChecksIt(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.dir = gameDir
	h.tty = true

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript(t, `{"$schema": "`+instance.SchemaURL+`", "settings": {"hooks": {}, "memory": "12G"}}`))
	h.mustRun(t, "instance", "edit")
	if s := instanceSettings(t, gameDir); s.Memory != "12G" {
		t.Fatalf("the edit is what the file holds now: %+v", s)
	}

	t.Setenv("EDITOR", editorScript(t, `{"$schema": "`+instance.SchemaURL+`", "settings": {"memory": "lots"}}`))
	if env := h.runSetting(t, 1, "instance", "edit"); env.Error == nil || env.Error.Code != "instance-invalid" {
		t.Fatalf("an edit that breaks the schema is reported: %+v", env.Error)
	}

	h.tty = false
	if env := h.runSetting(t, 2, "instance", "edit"); env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("with no terminal there is nobody to edit: %+v", env.Error)
	}
}

func TestConfigSetChecksThePlayDefaults(t *testing.T) {
	h := newHarness(t)
	for _, bad := range [][]string{
		{"play.memory", "4"},
		{"play.window", "1280"},
		{"play.jvmArgs", "-Dx=1"},
		{"play.wrapper", "gamemoderun"},
		{"play.java", "java"},
	} {
		if env := h.runSetting(t, 2, append([]string{"config", "set"}, bad...)...); env.Error == nil || env.Error.Code != "usage" {
			t.Fatalf("config set %v: %+v", bad, env.Error)
		}
	}
	h.mustRun(t, "config", "set", "play.memory", "4G")
	h.mustRun(t, "config", "set", "play.window", "1280x720")
	h.mustRun(t, "config", "set", "play.jvmArgs", "--literal", `["-Dx=1"]`)
	h.mustRun(t, "config", "set", "play.wrapper", "--literal", `["gamemoderun"]`)
	if got := strings.TrimSpace(h.mustRun(t, "config", "get", "play.window")); got != "1280x720" {
		t.Fatalf("play.window = %q", got)
	}
}

func TestConfigSaveBackups(t *testing.T) {
	h := newHarness(t)
	if got := strings.TrimSpace(h.mustRun(t, "config", "get", "play.saveBackups")); got != "5" {
		t.Fatalf("default play.saveBackups = %q", got)
	}
	for _, bad := range []string{"-1", "five", "2.5"} {
		if env := h.runSetting(t, 2, "config", "set", "play.saveBackups", bad); env.Error == nil || env.Error.Code != "usage" {
			t.Fatalf("config set play.saveBackups %s: %+v", bad, env.Error)
		}
	}
	h.mustRun(t, "config", "set", "play.saveBackups", "0")
	if got := strings.TrimSpace(h.mustRun(t, "config", "get", "play.saveBackups")); got != "0" {
		t.Fatalf("play.saveBackups = %q", got)
	}
	cfg, err := config.LoadFile(h.config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Play.Backups() != 0 {
		t.Fatalf("Backups() = %d", cfg.Play.Backups())
	}
}
