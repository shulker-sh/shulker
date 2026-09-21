package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/instance"
)

// playGame points an instance at a game that does what the test needs. It answers -version like the
// fake JDK does, so the launch takes it as the instance's own Java, and then runs body in the game
// directory in place of Minecraft.
func playGame(t *testing.T, gameDir, body string) {
	t.Helper()
	jdk := t.TempDir()
	if err := os.MkdirAll(filepath.Join(jdk, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"-version\" ]; then\n  echo 'openjdk version \"25.0.1\" 2025-10-21' >&2\n  exit 0\nfi\n" + body
	if err := os.WriteFile(filepath.Join(jdk, "bin", "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := instance.Load(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	f.Settings.Java = jdk
	if err := f.Save(gameDir); err != nil {
		t.Fatal(err)
	}
}

// crashingGame writes a crash report the way the game does, then exits with status.
func crashingGame(status string) string {
	return "mkdir -p crash-reports\necho boom > crash-reports/crash-client.txt\necho '[Render thread] crashed'\nexit " + status + "\n"
}

func onlyRun(t *testing.T, gameDir string) instance.Launch {
	t.Helper()
	records := instance.LoadLaunches(gameDir)
	if len(records) != 1 {
		t.Fatalf("one launch, one record: %+v", records)
	}
	return records[0]
}

func TestPlayDetachedRecordsTheRunWhenTheGameExits(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	playGame(t, gameDir, crashingGame("3"))

	res := playedJSON(t, h, "-i", "pack", "play")
	h.watching.Wait()

	run := onlyRun(t, gameDir)
	if run.Outcome != instance.OutcomeCrashed || run.ExitCode != 3 {
		t.Fatalf("the watcher records how the game ended: %+v", run)
	}
	if run.CrashReport != filepath.Join(gameDir, "crash-reports", "crash-client.txt") {
		t.Fatalf("crash report %q", run.CrashReport)
	}
	if run.Log != res.Log || run.StartedAt == "" || run.EndedAt == "" {
		t.Fatalf("record %+v, launch log %q", run, res.Log)
	}
	if run.PID != 0 {
		t.Fatalf("a closed record keeps no pid: %+v", run)
	}
	// The result came back while the game was still running, so it says nothing about how it went.
	if res.Outcome != "" || res.PID == 0 {
		t.Fatalf("result %+v", res)
	}
}

func TestPlayDetachedLeavesAnOlderCrashReportOutOfTheRecord(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	playGame(t, gameDir, "exit 0\n")
	old := filepath.Join(gameDir, "crash-reports", "crash-last-week.txt")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	lastWeek := time.Now().Add(-7 * 24 * time.Hour)
	if err := os.Chtimes(old, lastWeek, lastWeek); err != nil {
		t.Fatal(err)
	}

	h.mustRun(t, "-i", "pack", "play")
	h.watching.Wait()

	if run := onlyRun(t, gameDir); run.Outcome != instance.OutcomeOK || run.CrashReport != "" || run.ExitCode != 0 {
		t.Fatalf("a crash report from before the launch is some other run's: %+v", run)
	}
}

func TestPlayWaitRecordsTheRunBeforeItReturns(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	playGame(t, gameDir, crashingGame("5"))

	code, stdout, stderr := h.run(t, "-i", "pack", "play", "--wait")

	// A game that ran and crashed is the player's business: the launch itself did its job.
	if code != 0 {
		t.Fatalf("exit %d:\n%s%s", code, stdout, stderr)
	}
	for _, want := range []string{"pack crashed", "status: 5", "crash report: " + filepath.Join(gameDir, "crash-reports", "crash-client.txt")} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("play --wait: %q is missing from\n%s", want, stdout)
		}
	}
	// The command waited, so the record is closed already and no watcher was left behind to close it.
	if run := onlyRun(t, gameDir); run.Outcome != instance.OutcomeCrashed || run.ExitCode != 5 || run.EndedAt == "" {
		t.Fatalf("record %+v", run)
	}
	if strings.Contains(stdout, "[Render thread]") {
		t.Fatalf("--wait keeps the game's output in the log:\n%s", stdout)
	}

	// A crash report from this run's own first second counts, so the last one goes before the next.
	if err := os.RemoveAll(filepath.Join(gameDir, "crash-reports")); err != nil {
		t.Fatal(err)
	}
	playGame(t, gameDir, "exit 0\n")
	res := playedJSON(t, h, "-i", "pack", "play", "--wait", "--no-sync")
	if res.Outcome != instance.OutcomeOK || res.ExitCode != 0 || res.CrashReport != "" || res.PID == 0 {
		t.Fatalf("result %+v", res)
	}
}

func TestPlayStreamMirrorsTheGameAndStillWritesTheLog(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	playGame(t, gameDir, "echo '[Render thread] Setting user: Notch'\nexit 0\n")

	stdout := h.mustRun(t, "-i", "pack", "play", "--stream")

	if !strings.Contains(stdout, "[Render thread] Setting user: Notch") || !strings.Contains(stdout, "played pack") {
		t.Fatalf("play --stream shows the game as it runs, then how it ended:\n%s", stdout)
	}
	run := onlyRun(t, gameDir)
	if body, err := os.ReadFile(run.Log); err != nil || !strings.Contains(string(body), "Setting user: Notch") {
		t.Fatalf("the log is written either way: %v\n%s", err, body)
	}

	// Under --json the envelope owns stdout, so the game's output goes to stderr instead.
	code, stdout, stderr := h.run(t, "-i", "pack", "play", "--stream", "--no-sync", "--json")
	if code != 0 || !strings.Contains(stderr, "Setting user: Notch") {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var env struct {
		Data playResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || env.Data.Outcome != instance.OutcomeOK {
		t.Fatalf("stdout is the envelope alone: %v\n%s", err, stdout)
	}
}

// deadPID is a process that has exited and been reaped, which is what a game looks like once the
// watcher that was waiting on it has been killed and the game has gone too.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeRuns(t *testing.T, gameDir string, records ...instance.Launch) {
	t.Helper()
	if err := instance.SaveLaunches(gameDir, records, -1); err != nil {
		t.Fatal(err)
	}
}

func TestAKilledWatchersRunIsClosedByTheNextCommand(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	started := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	crash := filepath.Join(gameDir, "crash-reports", "crash-client.txt")
	if err := os.MkdirAll(filepath.Dir(crash), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crash, []byte("boom"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRuns(t, gameDir, instance.Launch{StartedAt: started.Format(time.RFC3339), Log: "/logs/run.log", PID: deadPID(t)})

	h.mustRun(t, "instances", "repair")

	run := onlyRun(t, gameDir)
	if run.EndedAt == "" || run.Outcome != instance.OutcomeCrashed || run.CrashReport != crash {
		t.Fatalf("the crash report is all a killed watcher left behind: %+v", run)
	}
	if run.PID != 0 || run.ExitCode != 0 || run.Log != "/logs/run.log" {
		t.Fatalf("record %+v", run)
	}
}

func TestPlayClosesTheRunAKilledWatcherLeftBeforeStartingItsOwn(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	playGame(t, gameDir, "exit 0\n")
	started := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeRuns(t, gameDir, instance.Launch{StartedAt: started, PID: deadPID(t)})

	h.mustRun(t, "-i", "pack", "play", "--wait", "--no-sync")

	records := instance.LoadLaunches(gameDir)
	if len(records) != 2 {
		t.Fatalf("the lost run and this one: %+v", records)
	}
	if lost := records[0]; lost.StartedAt != started || lost.Outcome != instance.OutcomeOK || lost.EndedAt == "" {
		t.Fatalf("the lost run, closed with no crash report to say otherwise: %+v", lost)
	}
	if this := records[1]; this.Outcome != instance.OutcomeOK || this.EndedAt == "" {
		t.Fatalf("this run: %+v", this)
	}
}

func TestReconcileLeavesARunThatIsStillGoingOpen(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	started := time.Now().UTC().Format(time.RFC3339)
	// This test's own process stands in for a game still running under a killed watcher, and a
	// record with no pid for one a launcher started, which only its own post-exit hook may close.
	for _, open := range []instance.Launch{
		{StartedAt: started, PID: os.Getpid()},
		{StartedAt: started},
	} {
		writeRuns(t, gameDir, open)

		h.mustRun(t, "instances", "repair")

		if run := onlyRun(t, gameDir); run.EndedAt != "" || run.Outcome != "" || run.PID != open.PID {
			t.Fatalf("reconcile closed a run it had no evidence had ended: %+v", run)
		}
	}
}

func TestAWatchedLaunchThatNeverStartedReachesTheInstanceList(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	missing := filepath.Join(t.TempDir(), "no-java")
	a := h.newApp(os.Stderr, os.Stderr)

	var reply watchReply
	rec := a.watchRun(watchRequest{Dir: gameDir, Java: missing, Log: filepath.Join(gameDir, instance.Dir, "logs", "x.log")}, nil, func(r watchReply) { reply = r })

	if reply.PID != 0 || !strings.HasPrefix(reply.Error, "run "+missing+": ") {
		t.Fatalf("the watcher answers with why nothing started: %+v", reply)
	}
	if rec.Outcome != instance.OutcomeNotStarted {
		t.Fatalf("returned %+v", rec)
	}
	run := onlyRun(t, gameDir)
	if run.Outcome != instance.OutcomeNotStarted || run.Error != reply.Error || run.StartedAt != run.EndedAt {
		t.Fatalf("recorded %+v", run)
	}
	var env struct {
		Data []instanceEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || env.Data[0].LaunchError != reply.Error {
		t.Fatalf("instances shows a watched launch like a launcher's: %+v", env.Data)
	}
}

func TestTheWatcherReadsItsLaunchFromStdinAndAnswersWithOneLine(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	playGame(t, gameDir, "exit 0\n")
	f, err := instance.Load(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(watchRequest{Dir: gameDir, Java: filepath.Join(f.Settings.Java, "bin", "java"), Argv: []string{"--accessToken", "mc-secret"}, Log: filepath.Join(gameDir, instance.Dir, "logs", "w.log")})
	h.stdin = strings.NewReader(string(req))

	code, stdout, stderr := h.run(t, "watch")

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	var reply watchReply
	if lines := strings.Split(strings.TrimSpace(stdout), "\n"); len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &reply) != nil || reply.PID == 0 {
		t.Fatalf("one line, with the game's pid in it: %q", stdout)
	}
	if run := onlyRun(t, gameDir); run.Outcome != instance.OutcomeOK || run.EndedAt == "" {
		t.Fatalf("record %+v", run)
	}
	if strings.Contains(stdout+stderr, "mc-secret") {
		t.Fatal("the watcher repeated the session token")
	}
}
