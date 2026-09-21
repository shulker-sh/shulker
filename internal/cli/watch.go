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
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
)

// watchRequest is the launch `play` hands the watcher. It travels over the watcher's stdin and never
// its argv, because Argv holds the session access token and a process table is public.
type watchRequest struct {
	Dir  string   `json:"dir"`
	Java string   `json:"java"`
	Argv []string `json:"argv"`
	Log  string   `json:"log"`
}

// watchReply is the one line the watcher writes back before it settles down to wait, so that `play`
// can report the process it started, or say why nothing did.
type watchReply struct {
	PID   int    `json:"pid,omitempty"`
	Error string `json:"error,omitempty"`
}

// watchCmd is the watcher: shulker re-execed hidden and detached so that a run nobody is waiting for
// still ends in a record. It is hidden because its one argument is a launch on stdin, which nothing
// but `play` has to hand.
func (a *app) watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "watch",
		Short:  "Wait for a detached game and record how the run ended",
		Hidden: true,
		Args:   noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req watchRequest
			if err := json.NewDecoder(a.stdin).Decode(&req); err != nil {
				return out.Errorf("usage", "the watcher reads one launch from its stdin: %v", err)
			}
			// The line below is the last thing this process writes anywhere but the record: whoever
			// asked for the launch has gone by the time the game exits, and writing to a pipe with
			// nobody on the other end would end the watch.
			a.watchRun(req, nil, func(r watchReply) {
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
// is who running answers, and whether anyone is still listening once the game has gone.
func (a *app) watchRun(req watchRequest, stream io.Writer, running func(watchReply)) instance.Launch {
	var s instance.Settings
	if f, err := instance.Load(req.Dir); err == nil {
		s = f.Settings
	}
	// File times can be coarser than the clock, so a crash report from this run's first second counts.
	started := time.Now().Truncate(time.Second)
	g, err := game.Start(game.Launch{Java: req.Java, Argv: req.Argv, Dir: req.Dir, Log: req.Log}, stream)
	if err != nil {
		reason := runReason(req.Java, err)
		a.failLaunch(req.Dir, s, false, reason)
		running(watchReply{Error: reason})
		return instance.Launch{Outcome: instance.OutcomeNotStarted, Error: reason}
	}
	a.openRun(req.Dir, s, instance.Launch{
		StartedAt: started.UTC().Format(time.RFC3339),
		Log:       req.Log,
		PID:       g.PID,
	})
	running(watchReply{PID: g.PID})
	code, err := g.Wait()
	if err != nil {
		// The game ran, so the run is real; what shulker lost is only the status it ended with.
		code = noExitCode
	}
	return a.closeRun(req.Dir, s, code)
}

// startWatcher hands the launch to the watcher and reports the game it started. Tests replace it,
// because a test binary re-execed is a test binary and not shulker.
func (a *app) startWatcher(req watchRequest) (int, error) {
	if a.watcher != nil {
		return a.watcher(req)
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
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
	return reply.PID, nil
}
