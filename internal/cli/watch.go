package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
)

// watchReply is the one line the watcher writes back before it settles down to wait, so that `play`
// can report the process it started, or say why nothing did.
type watchReply struct {
	PID   int    `json:"pid,omitempty"`
	Error string `json:"error,omitempty"`
	// Exited is a game that failed within the grace period, with its status and its log's last line.
	Exited   bool   `json:"exited,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	LastLine string `json:"lastLine,omitempty"`
}

// earlyExitGrace is how long the watcher waits before saying the game started, so a game or wrapper
// that fails at once is reported as a failure rather than a launch.
const earlyExitGrace = 2 * time.Second

// watchCmd is the watcher: shulker re-execed hidden and detached so that a run nobody is waiting for
// still ends in a record. It is hidden because its one argument is a launch on stdin, which nothing
// but `play` has to hand. The launch travels over stdin and never argv, because its argv holds the
// session access token and a process table is public.
func (a *app) watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "watch",
		Annotations: acts(),
		Short:       "Wait for a detached game and record how the run ended",
		Hidden:      true,
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.printer.Raw()
			var req game.Launch
			if err := json.NewDecoder(a.stdin).Decode(&req); err != nil {
				return out.Errorf("usage", "the watcher reads one launch from its stdin").WithCause("json", err)
			}
			// The line below is the last thing this process writes anywhere but the record: whoever
			// asked for the launch has gone by the time the game exits, and writing to a pipe with
			// nobody on the other end would end the watch.
			a.watchRun(req, nil, earlyExitGrace, func(r watchReply) {
				if line, err := json.Marshal(r); err == nil {
					fmt.Fprintln(a.printer.Stdout, string(line))
				}
			})
			return nil
		},
	}
}

// watchRun is the watched run itself: start the game, say what started, wait for it, and record how
// it ended. The watcher and a foreground `play --wait` are both this function — the only difference
// is who running answers, and whether anyone is still listening once the game has gone. With a
// grace period, running waits that long first, and a game that failed within it is said to have.
func (a *app) watchRun(req game.Launch, stream io.Writer, grace time.Duration, running func(watchReply)) instance.Launch {
	var s instance.Settings
	if f, err := instance.Load(req.Dir); err == nil {
		s = f.Settings
	}
	// File times can be coarser than the clock, so a crash report from this run's first second counts.
	started := time.Now().Truncate(time.Second)
	launch := game.Launch{Java: req.Java, Argv: req.Argv, Dir: req.Dir, Log: req.Log, Wrapper: req.Wrapper}
	g, err := game.Start(launch, stream)
	if err != nil {
		reason := runReason(launch.Program(), err)
		if err := instance.FailLaunch(req.Dir, s.LaunchKeep(), false, reason); err != nil {
			a.printer.Warn("%v", err)
		}
		running(watchReply{Error: reason})
		return instance.Launch{Outcome: instance.OutcomeNotStarted, Error: reason}
	}
	err = instance.OpenRun(req.Dir, s.LaunchKeep(), instance.Launch{
		StartedAt: started.UTC().Format(time.RFC3339),
		Log:       req.Log,
		PID:       g.PID,
		Java:      req.Java,
		Wrapped:   len(req.Wrapper) > 0,
	})
	if err != nil {
		a.printer.Warn("%v", err)
	}
	ended := make(chan int, 1)
	go func() {
		code, err := g.Wait()
		if err != nil {
			// The game ran, so the run is real; what shulker lost is only the status it ended with.
			code = instance.NoExitCode
		}
		ended <- code
	}()
	closeRun := func(code int) instance.Launch {
		rec, err := instance.CloseRun(req.Dir, s.LaunchKeep(), g.PID, code)
		if err != nil {
			a.printer.Warn("%v", err)
		}
		return rec
	}
	if grace > 0 {
		select {
		case code := <-ended:
			rec := closeRun(code)
			if code == 0 || code == instance.NoExitCode {
				running(watchReply{PID: g.PID})
			} else {
				running(watchReply{PID: g.PID, Exited: true, ExitCode: code, LastLine: lastLogLine(req.Log)})
			}
			return rec
		case <-time.After(grace):
		}
	}
	running(watchReply{PID: g.PID})
	return closeRun(<-ended)
}

// lastLogLine is the last line a run wrote to its log, which for a game that failed at once is
// usually why.
func lastLogLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	tail, err := fsutil.ReadTail(f, 1)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// startWatcher hands the launch to the watcher and reports the game it started. Tests replace it,
// because a test binary re-execed is a test binary and not shulker.
func (a *app) startWatcher(req game.Launch) (int, error) {
	if a.watcher != nil {
		return a.watcher(req)
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, notStarted("can't find shulker to start the watcher with: " + err.Error())
	}
	body, err := json.Marshal(req)
	if err != nil {
		return 0, notStarted(err.Error())
	}
	line, err := game.Watch(exe, []string{"watch"}, body)
	if err != nil {
		return 0, notStarted(err.Error())
	}
	var reply watchReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return 0, notStarted("the watcher answered with " + strconv.Quote(strings.TrimSpace(string(line))))
	}
	if reply.Error != "" {
		return 0, notStarted(reply.Error)
	}
	if reply.Exited {
		return 0, exitedAtStart(reply, req.Log)
	}
	return reply.PID, nil
}

func exitedAtStart(r watchReply, log string) *out.Error {
	e := out.Errorf("game-exited", "Minecraft exited as it started (exit code %d)", r.ExitCode)
	if r.LastLine != "" {
		e.Message += ": " + r.LastLine
	}
	e.Rows = []out.Detail{{Label: "log", Text: log}}
	return e
}
