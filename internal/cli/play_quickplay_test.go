package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlayWorldBootsIntoTheSave(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	if argv := playedArgv(t, h, gameDir, "--world", "New World"); !strings.Contains(argv, "--quickPlaySingleplayer\nNew World\n") {
		t.Fatalf("--world becomes --quickPlaySingleplayer:\n%s", argv)
	}
	if argv := playedArgv(t, h, gameDir); strings.Contains(argv, "--quickPlay") {
		t.Fatalf("a launch with no target asks for none:\n%s", argv)
	}
}

func TestPlayServerJoinsTheServer(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	if argv := playedArgv(t, h, gameDir, "--server", "mc.example.com:25570"); !strings.Contains(argv, "--quickPlayMultiplayer\nmc.example.com:25570\n") {
		t.Fatalf("--server becomes --quickPlayMultiplayer:\n%s", argv)
	}
	if argv := playedArgv(t, h, gameDir, "--server", "mc.example.com"); !strings.Contains(argv, "--quickPlayMultiplayer\nmc.example.com\n") {
		t.Fatalf("a server with no port is passed as given:\n%s", argv)
	}
}

func TestPlayServerFallsBackToTheOldPairWithoutQuickPlay(t *testing.T) {
	h := newHarness(t)
	h.noQuickPlay = true
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	if argv := playedArgv(t, h, gameDir, "--server", "mc.example.com:25570"); !strings.Contains(argv, "--server\nmc.example.com\n--port\n25570\n") || strings.Contains(argv, "--quickPlay") {
		t.Fatalf("a version with no quick play gets --server and --port:\n%s", argv)
	}
	if argv := playedArgv(t, h, gameDir, "--server", "[::1]"); !strings.Contains(argv, "--server\n::1\n--port\n25565\n") {
		t.Fatalf("a server with no port joins on the default one:\n%s", argv)
	}
}

func TestPlayWorldFailsOnAVersionWithoutQuickPlay(t *testing.T) {
	h := newHarness(t)
	h.noQuickPlay = true
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	env := h.runSetting(t, 1, "-i", "pack", "play", "--no-sync", "--world", "New World")
	if env.Error == nil || env.Error.Code != "unsupported-quickplay" || !strings.Contains(env.Error.Message, "26.2") || !strings.Contains(env.Error.Message, "1.20") {
		t.Fatalf("--world without quick play fails naming the version and the floor: %+v", env.Error)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "args.txt")); !os.IsNotExist(err) {
		t.Fatalf("nothing was launched: %v", err)
	}
	if env := h.runSetting(t, 1, "-i", "pack", "play", "--dry-run", "--world", "New World"); env.Error == nil || env.Error.Code != "unsupported-quickplay" {
		t.Fatalf("a dry run fails the same way: %+v", env.Error)
	}
}

func TestPlayQuickPlayUsageErrors(t *testing.T) {
	h := newHarness(t)
	playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	for _, args := range [][]string{
		{"--world", "New World", "--server", "mc.example.com"},
		{"--server", "mc.example.com:port"},
		{"--server", "mc.example.com:70000"},
		{"--server", ":25565"},
		{"--world", ""},
	} {
		if code, stdout, _ := h.run(t, append([]string{"-i", "pack", "play", "--no-sync", "--json"}, args...)...); code != 2 || !strings.Contains(stdout, `"usage"`) {
			t.Fatalf("%v: exit %d\n%s", args, code, stdout)
		}
	}
}
