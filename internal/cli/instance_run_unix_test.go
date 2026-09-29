//go:build !windows

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
)

const fakeDump = `Full thread dump OpenJDK 64-Bit Server VM (21.0.7+6-LTS mixed mode):

"main" #1 [259] prio=5 runnable
   java.lang.Thread.State: RUNNABLE
	at toni.missingmodschecker.MissingModsWindow.open(MissingModsWindow.java:40)

"Render thread" #30 [43267] prio=5 waiting on condition
   java.lang.Thread.State: TIMED_WAITING (sleeping)
	at java.lang.Thread.sleep0(java.base@21.0.7/Native Method)

"Worker-Main-1" #40 prio=5 waiting on condition
	at jdk.internal.misc.Unsafe.park(java.base@21.0.7/Native Method)

JNI global refs: 23, weak refs: 0
`

// dumpingGame is a process that prints a thread dump into log on SIGQUIT, the way HotSpot does.
func dumpingGame(t *testing.T, log string) int {
	t.Helper()
	dump := filepath.Join(t.TempDir(), "dump.txt")
	if err := os.WriteFile(dump, []byte(fakeDump), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	cmd := exec.Command("sh", "-c", `trap 'cat "$0"' QUIT; echo ready; while :; do sleep 0.05; done`, dump)
	cmd.Stdout = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for deadline := time.Now().Add(2 * time.Second); ; {
		if data, _ := os.ReadFile(log); strings.Contains(string(data), "ready") {
			return cmd.Process.Pid
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake game never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInstanceDumpPrintsTheMainAndRenderThreads(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	log := runLog(t, gameDir, "")
	pid := dumpingGame(t, log)
	writeRuns(t, gameDir, instance.Launch{StartedAt: "2026-09-23T00:02:02Z", Log: log, PID: pid})

	stdout := h.mustRun(t, "instance", "dump", "-i", "pack")
	for _, want := range []string{"MissingModsWindow.open", `"Render thread" #30`, log} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dump shows %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Worker-Main-1") {
		t.Fatalf("the other threads stay in the log:\n%s", stdout)
	}

	var env struct {
		Data struct {
			PID     int           `json:"pid"`
			Log     string        `json:"log"`
			Threads []game.Thread `json:"threads"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instance", "dump", "-i", "pack", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.PID != pid || env.Data.Log != log || len(env.Data.Threads) != 3 {
		t.Fatalf("json: %+v", env.Data)
	}
	if body, _ := os.ReadFile(log); strings.Count(string(body), "Full thread dump") != 2 {
		t.Fatalf("each dump lands in the run's log:\n%s", body)
	}
}

func TestInstanceDumpRefusesAWrappedRunUnlessGivenJavasPid(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	log := runLog(t, gameDir, "")
	wrapper := os.Getpid()
	java := dumpingGame(t, log)
	writeRuns(t, gameDir, instance.Launch{StartedAt: "2026-09-23T00:02:02Z", Log: log, PID: wrapper, Wrapped: true})

	e := runError(t, h, "instance", "dump", "-i", "pack")
	if e.Code != "game-wrapped" || !strings.Contains(e.Help, "--pid") {
		t.Fatalf("a wrapped run is refused, with the way round it: %+v", e)
	}
	if body, _ := os.ReadFile(log); strings.Contains(string(body), "Full thread dump") {
		t.Fatal("nothing was signalled")
	}

	stdout := h.mustRun(t, "instance", "dump", "-i", "pack", "--pid", strconv.Itoa(java))
	if !strings.Contains(stdout, "MissingModsWindow.open") {
		t.Fatalf("--pid dumps the process it names:\n%s", stdout)
	}
	if e := runError(t, h, "instance", "dump", "-i", "pack", "--pid", strconv.Itoa(deadPID(t))); e.Code != "game-not-running" {
		t.Fatalf("--pid of a process that has gone: %+v", e)
	}
}
