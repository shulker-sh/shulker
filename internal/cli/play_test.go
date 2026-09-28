package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/instance"
)

// playHarness links a shulker instance with its own store root and leaves the harness addressing it
// by nickname, which is how a launch is reached from outside the project directory.
func playHarness(t *testing.T, h *harness, args ...string) (store, gameDir string) {
	t.Helper()
	root := shulkerInstances(t, h)
	store = filepath.Join(t.TempDir(), "store")
	h.mustRun(t, "config", "set", "store", store)
	h.mustRun(t, append([]string{"create", "--name", "pack"}, args...)...)
	h.mustRun(t, "link", "shulker")
	h.dir = ""
	return store, filepath.Join(root, "pack")
}

func playJSON(t *testing.T, h *harness, args ...string) playReport {
	t.Helper()
	stdout := h.mustRun(t, append(args, "--json")...)
	var env struct {
		Data playReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestPlayDryRunFillsTheStore(t *testing.T) {
	h := newHarness(t)
	store, gameDir := playHarness(t, h)

	stdout, stderr := h.mustRunStderr(t, "-i", "pack", "play", "--dry-run")

	for _, want := range []string{"Would launch pack", "26.2", "main class: net.minecraft.client.main.Main", "memory: " + instance.DefaultMemory, "asset index: 26", "classpath: 2 jars"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("play --dry-run: %q is missing from\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "inherits") {
		t.Fatalf("a vanilla instance inherits from nothing: %s", stdout)
	}
	// The game's own arguments carry a session access token, so no part of them is ever printed.
	for _, leak := range []string{"--username", "--accessToken", "auth_access_token", "-cp"} {
		if strings.Contains(stdout+stderr, leak) {
			t.Fatalf("the argv leaked %q into the output:\n%s%s", leak, stdout, stderr)
		}
	}

	assets := sha1Hex([]byte(h.assets["icons/icon_16x16.png"]))
	for _, rel := range []string{
		filepath.Join("versions", "26.2", "26.2.json"),
		filepath.Join("versions", "26.2", "26.2.jar"),
		filepath.Join("libraries", "com", "mojang", "brigadier", "1.3.10", "brigadier-1.3.10.jar"),
		filepath.Join("assets", "indexes", "26.json"),
		filepath.Join("assets", "objects", assets[:2], assets),
		"launcher_profiles.json",
	} {
		if _, err := os.Stat(filepath.Join(store, rel)); err != nil {
			t.Fatalf("the store is missing %s: %v", rel, err)
		}
	}

	rep := playJSON(t, h, "-i", "pack", "play", "--dry-run")
	if rep.Instance != "pack" || rep.Version != "26.2" || rep.Inherits != "" || rep.AssetIndex != "26" {
		t.Fatalf("report %+v", rep)
	}
	if rep.GameDir != gameDir || rep.NativesDir != filepath.Join(gameDir, instance.Dir, "natives") {
		t.Fatalf("directories %+v", rep)
	}
	if rep.Classpath != 2 || rep.ClasspathBytes == 0 || rep.Java == "" {
		t.Fatalf("classpath %+v", rep)
	}

	// Everything is in the store now, so a re-run reaches the network for nothing.
	before := h.storeHits
	h.mustRun(t, "-i", "pack", "play", "--dry-run")
	if h.storeHits != before {
		t.Fatalf("a re-run downloaded %d files", h.storeHits-before)
	}
	if _, err := os.Stat(filepath.Join(store, "loaders.json")); err == nil {
		t.Fatal("a vanilla instance runs no loader installer")
	}
}

func TestPlayRefusesAnotherLaunchersInstance(t *testing.T) {
	h := newHarness(t)
	launcherDir := t.TempDir()
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--name", "Friends")
	h.dir = ""

	code, stdout, _ := h.run(t, "-i", "friends", "play", "--dry-run", "--json")

	if code == 0 || !strings.Contains(stdout, `"not-shulker"`) {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

// waitForFile reads a file a detached game writes. `play` returns before the game has run at all,
// so a launch is checked by what the process left behind rather than by what the command printed.
func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never turned up: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// launchLogs is every log a launch left in the instance, newest name last.
func launchLogs(t *testing.T, gameDir string) []string {
	t.Helper()
	dir := filepath.Join(gameDir, instance.Dir, "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no launch logs: %v", err)
	}
	var logs []string
	for _, e := range entries {
		logs = append(logs, filepath.Join(dir, e.Name()))
	}
	return logs
}

func playedJSON(t *testing.T, h *harness, args ...string) playResult {
	t.Helper()
	stdout := h.mustRun(t, append(args, "--json")...)
	var env struct {
		Data playResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestPlayStartsTheGameDetachedAndLogsIt(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	stdout, stderr := h.mustRunStderr(t, "-i", "pack", "play")

	for _, want := range []string{"Launched pack as Notch (Minecraft 26.2)", "log: "} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("play: %q is missing from\n%s", want, stdout)
		}
	}
	if strings.Contains(stderr, "Started") || strings.Contains(stdout, "account:") {
		t.Fatalf("the launched line names the account, and no other line or row repeats it:\n%s%s", stderr, stdout)
	}
	if strings.Contains(stdout, "Synced") {
		t.Fatalf("a sync that changed nothing says nothing:\n%s", stdout)
	}
	// The game ran with the account templated into its own arguments, and in the game directory.
	argv := waitForFile(t, filepath.Join(gameDir, "args.txt"))
	for _, want := range []string{"--username\nNotch\n", "--accessToken\nmc-notch\n", "net.minecraft.client.main.Main\n"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("the game's argv is missing %q:\n%s", want, argv)
		}
	}
	// Whatever it wrote went to the log, whether or not anyone was watching.
	logs := launchLogs(t, gameDir)
	if len(logs) != 1 {
		t.Fatalf("one launch, one log: %v", logs)
	}
	if body := waitForFile(t, logs[0]); !strings.Contains(body, "[Server] Done") {
		t.Fatalf("the game's output is missing from %s:\n%s", logs[0], body)
	}
	// The argv carries the session token, so no part of it reaches the output or the log.
	for _, leak := range []string{"mc-notch", "--accessToken", "--username", "-cp"} {
		if strings.Contains(stdout+stderr, leak) {
			t.Fatalf("the argv leaked %q into the output:\n%s%s", leak, stdout, stderr)
		}
		if body, _ := os.ReadFile(logs[0]); strings.Contains(string(body), leak) {
			t.Fatalf("the argv leaked %q into the log:\n%s", leak, body)
		}
	}

	res := playedJSON(t, h, "-i", "pack", "play")
	if res.Instance != "pack" || res.Version != "26.2" || res.GameDir != gameDir || res.PID == 0 {
		t.Fatalf("result %+v", res)
	}
	if res.Account.Name != "Notch" || res.Account.ID != notchID || !res.Account.Default {
		t.Fatalf("account %+v", res.Account)
	}
	if dir := filepath.Dir(res.Log); dir != filepath.Join(gameDir, instance.Dir, "logs") {
		t.Fatalf("log %q", res.Log)
	}
	if len(launchLogs(t, gameDir)) != 2 {
		t.Fatalf("a second launch writes a second log: %v", launchLogs(t, gameDir))
	}
}

func TestPlayWithNoDefaultTakesTheOnlyAccountThereIs(t *testing.T) {
	h := newHarness(t)
	playHarness(t, h)
	h.mustRun(t, "accounts", "login")
	h.mustRun(t, "config", "unset", "accounts.default")

	stdout := h.mustRun(t, "-i", "pack", "play")

	if !strings.Contains(stdout, "Notch is the default account now") {
		t.Fatalf("a launch that settles the default account says so:\n%s", stdout)
	}
	if got := h.mustRun(t, "config", "get", "accounts.default"); !strings.Contains(got, notchID) {
		t.Fatalf("accounts.default = %q", got)
	}
	// Settled, so the next launch says nothing about it.
	if stdout := h.mustRun(t, "-i", "pack", "play"); strings.Contains(stdout, "default account now") {
		t.Fatalf("the question is asked once:\n%s", stdout)
	}
}
