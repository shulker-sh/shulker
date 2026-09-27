package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/instance"
)

func runLog(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, instance.Dir, "logs", "20260923-000202.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstanceDumpFailsWhenNoGameIsRunning(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)

	if e := runError(t, h, "-i", "pack", "instance", "dump"); e.Code != "game-not-running" {
		t.Fatalf("no run at all: %+v", e)
	}

	log := runLog(t, gameDir, "")
	writeRuns(t, gameDir,
		instance.Launch{StartedAt: "2026-09-23T00:00:00Z", EndedAt: "2026-09-23T00:01:00Z", Outcome: instance.OutcomeOK, Log: log},
		instance.Launch{StartedAt: "2026-09-23T00:02:02Z"},
	)
	if e := runError(t, h, "-i", "pack", "instance", "dump"); e.Code != "game-not-running" {
		t.Fatalf("a closed run, and a launcher's open one with no pid: %+v", e)
	}

	writeRuns(t, gameDir, instance.Launch{StartedAt: "2026-09-23T00:02:02Z", Log: log, PID: deadPID(t)})
	if e := runError(t, h, "-i", "pack", "instance", "dump"); e.Code != "game-not-running" {
		t.Fatalf("an open run whose game has gone: %+v", e)
	}
}

func TestInstanceLogPrintsTheLatestRunsLog(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)

	if e := runError(t, h, "-i", "pack", "instance", "log"); e.Code != "run-not-found" {
		t.Fatalf("no run yet: %+v", e)
	}

	older := filepath.Join(gameDir, instance.Dir, "logs", "older.log")
	log := runLog(t, gameDir, "one\ntwo\nthree\n")
	writeRuns(t, gameDir,
		instance.Launch{StartedAt: "2026-09-23T00:00:00Z", EndedAt: "2026-09-23T00:01:00Z", Outcome: instance.OutcomeOK, Log: older},
		instance.Launch{StartedAt: "2026-09-23T00:02:02Z", EndedAt: "2026-09-23T00:03:00Z", Outcome: instance.OutcomeOK, Log: log},
	)

	if got := h.mustRun(t, "-i", "pack", "instance", "log"); got != "one\ntwo\nthree\n" {
		t.Fatalf("the newest run's log, as written:\n%q", got)
	}
	if got := h.mustRun(t, "-i", "pack", "instance", "log", "--limit", "2"); got != "two\nthree\n" {
		t.Fatalf("--limit keeps the last lines:\n%q", got)
	}
	var env struct {
		Data instanceLog `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "-i", "pack", "instance", "log", "--limit", "1", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Log != log || strings.Join(env.Data.Lines, ",") != "three" {
		t.Fatalf("json: %+v", env.Data)
	}
}

func TestInstanceLogFollowsUntilTheRunCloses(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	log := runLog(t, gameDir, "one\ntwo\n")
	writeRuns(t, gameDir, instance.Launch{StartedAt: "2026-09-23T00:02:02Z", Log: log, PID: os.Getpid()})

	type result struct {
		code   int
		stdout string
	}
	done := make(chan result, 1)
	go func() {
		code, stdout, _ := h.run(t, "-i", "pack", "instance", "log", "-f", "--limit", "1")
		done <- result{code, stdout}
	}()

	appendTo := func(text string) {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		f.WriteString(text)
	}
	time.Sleep(300 * time.Millisecond)
	appendTo("three\n")
	time.Sleep(300 * time.Millisecond)
	select {
	case r := <-done:
		t.Fatalf("follow stopped while the run was open: %+v", r)
	default:
	}
	appendTo("four\n")
	writeRuns(t, gameDir, instance.Launch{StartedAt: "2026-09-23T00:02:02Z", EndedAt: "2026-09-23T00:03:00Z", Outcome: instance.OutcomeOK, Log: log})

	select {
	case r := <-done:
		if r.code != 0 || r.stdout != "two\nthree\nfour\n" {
			t.Fatalf("follow prints the last line, then each new one to the end: %d %q", r.code, r.stdout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow kept going after the run closed")
	}
}

func TestPlayShowsTheGamesPidAndHowToDumpIt(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")

	stdout := h.mustRun(t, "-i", "pack", "play")
	waitForFile(t, filepath.Join(gameDir, "args.txt"))
	if !regexp.MustCompile(`pid: [1-9][0-9]*\n`).MatchString(stdout) {
		t.Fatalf("a detached launch shows the game's pid:\n%s", stdout)
	}
	for _, want := range []string{"If it hangs:", "$ shulker instance dump -i pack"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("a detached launch shows %q:\n%s", want, stdout)
		}
	}
}

func TestInstanceLogInAProjectReadsItsOneInstance(t *testing.T) {
	h := newHarness(t)
	project := h.dir
	_, gameDir := playHarness(t, h)
	h.dir = project
	writeRuns(t, gameDir, instance.Launch{StartedAt: "2026-09-23T00:02:02Z", EndedAt: "2026-09-23T00:03:00Z", Outcome: instance.OutcomeOK, Log: runLog(t, gameDir, "one\n")})

	if got := h.mustRun(t, "instance", "log"); got != "one\n" {
		t.Fatalf("the project's one instance's log:\n%q", got)
	}
}
