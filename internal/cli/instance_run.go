package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/proc"
)

// dumpShown are the threads `instance dump` prints: the one that starts the game and the one that
// draws it, which between them are where a frozen start or a slow quit sits.
var dumpShown = []string{"main", "Render thread"}

// followInterval is how often `instance log -f` looks for new output and for the run having ended.
var followInterval = 200 * time.Millisecond

type instanceDump struct {
	PID     int           `json:"pid"`
	Log     string        `json:"log"`
	Threads []game.Thread `json:"threads"`
}

type instanceLog struct {
	Log   string   `json:"log"`
	Lines []string `json:"lines"`
}

func (a *app) instanceDumpCmd() *cobra.Command {
	var pid int
	cmd := &cobra.Command{
		Use:         "dump",
		Annotations: acts(),
		Short:       "Print where the running game's main and render threads are, from a thread dump",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := a.instanceDir()
			if err != nil {
				return err
			}
			rec, err := runningGame(dir)
			if err != nil {
				return err
			}
			if pid != 0 {
				if !proc.IsAlive(pid) {
					return out.Errorf("game-not-running", "no process has pid %d", pid)
				}
				rec.PID = pid
			} else if rec.Wrapped {
				return gameWrapped(rec.PID)
			}
			text, err := game.Dump(rec.PID, rec.Java, rec.Log)
			if err != nil {
				return dumpFailed(err)
			}
			res := instanceDump{PID: rec.PID, Log: rec.Log, Threads: game.Threads(text)}
			return a.printer.Emit(res, func(l *out.Lines) {
				for _, th := range res.Threads {
					if slices.Contains(dumpShown, th.Name) {
						l.Raw(th.Stack)
						l.Blank()
					}
				}
				l.OK("dumped every thread into the log", "pid "+strconv.Itoa(res.PID))
				l.Tree(out.Row{Label: "log", Text: res.Log})
			})
		},
	}
	cmd.Flags().IntVar(&pid, "pid", 0, "dump this Java process instead, for a game started through a wrapper")
	return cmd
}

// gameWrapped refuses a game started through settings.wrapper: its pid is the wrapper's, and a
// wrapper that runs Java as its child rather than becoming it would be killed by the signal that
// asks Java for a dump, leaving the game running with nothing watching it.
func gameWrapped(pid int) error {
	e := out.Errorf("game-wrapped", "the game runs under a wrapper, so shulker knows only the wrapper's pid")
	e.Rows = append(e.Rows, out.Detail{Label: "wrapper pid", Text: strconv.Itoa(pid)})
	e.Help = "find the java process the wrapper started and pass its pid with --pid"
	return e
}

// runningGame is the open record of a game shulker started and is still running. A launcher's run
// has no pid to signal, so it is no more a running game here than a run that has ended.
func runningGame(dir string) (instance.Launch, error) {
	records := instance.LoadLaunches(dir)
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.EndedAt == "" && rec.PID != 0 && proc.IsAlive(rec.PID) {
			return rec, nil
		}
	}
	e := out.Errorf("game-not-running", "no game is running in %s", dir)
	e.Help = "shulker dumps a game it started itself, with shulker play"
	return instance.Launch{}, e
}

func dumpFailed(err error) error {
	var jcmd *game.JcmdNotFoundError
	switch {
	case errors.As(err, &jcmd):
		e := out.Errorf("jcmd-not-found", "the game's Java has no jcmd to take a thread dump with")
		e.Rows = append(e.Rows, out.Detail{Label: "looked for", Text: jcmd.Path})
		return e
	case errors.Is(err, game.ErrDumpTimeout):
		e := out.Errorf("dump-timeout", "the game printed no thread dump")
		e.Help = "a JVM started with -Xrs ignores the request"
		return e
	}
	return err
}

func (a *app) instanceLogCmd() *cobra.Command {
	var follow bool
	var limit int
	cmd := &cobra.Command{
		Use:         "log",
		Annotations: reads(),
		Short:       "Print the game's output from the latest run",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 0 {
				return out.Errorf("usage", "--limit takes a number of lines to print, not %d", limit)
			}
			if follow && a.printer.JSON {
				return out.Errorf("usage", "--follow prints the log as it grows, which --json can't")
			}
			dir, err := a.instanceDir()
			if err != nil {
				return err
			}
			rec, err := latestRun(dir)
			if err != nil {
				return err
			}
			f, err := os.Open(rec.Log)
			if errors.Is(err, fs.ErrNotExist) {
				return out.Errorf("run-not-found", "the latest run's log is gone: %s", rec.Log)
			} else if err != nil {
				return err
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			text := string(data)
			var partial string
			if follow {
				cut := strings.LastIndexByte(text, '\n') + 1
				text, partial = text[:cut], text[cut:]
			}
			lines := lastLines(text, limit)
			if !follow {
				res := instanceLog{Log: rec.Log, Lines: lines}
				return a.printer.Emit(res, func(l *out.Lines) {
					for _, line := range lines {
						l.Raw(line)
					}
				})
			}
			for _, line := range lines {
				io.WriteString(a.printer.Stdout, line+"\n")
			}
			return a.followLog(cmd.Context(), dir, rec, f, partial)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new output until the game exits")
	cmd.Flags().IntVar(&limit, "limit", 0, "print only the last N lines")
	return cmd
}

// latestRun is the newest record, which must have a log to read. A launcher's run gets its log only once it ends, so while it is open its log is the game's own
// latest.log.
func latestRun(dir string) (instance.Launch, error) {
	records := instance.LoadLaunches(dir)
	if len(records) > 0 {
		rec := records[len(records)-1]
		if rec.Log == "" && rec.EndedAt == "" {
			rec.Log = filepath.Join(dir, "logs", "latest.log")
		}
		if rec.Log != "" {
			return rec, nil
		}
	}
	e := out.Errorf("run-not-found", "%s has no run with a log yet", dir)
	e.Help = "start it with shulker play"
	return instance.Launch{}, e
}

// followLog prints what the game writes to its log from where f stands, until the run closes or the
// command is interrupted, starting with partial, a line the game had only begun. After the run
// closes it reads once more, for what the game wrote last.
func (a *app) followLog(ctx context.Context, dir string, rec instance.Launch, f *os.File, partial string) error {
	r := bufio.NewReader(f)
	drain := func() error {
		for {
			chunk, err := r.ReadString('\n')
			partial += chunk
			if errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return err
			}
			io.WriteString(a.printer.Stdout, partial)
			partial = ""
		}
	}
	for {
		if err := drain(); err != nil {
			return err
		}
		if rec.EndedAt != "" || runClosed(dir, rec) {
			if err := drain(); err != nil {
				return err
			}
			if partial != "" {
				io.WriteString(a.printer.Stdout, partial+"\n")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(followInterval):
		}
	}
}

// runClosed reports whether the run being followed has ended: its record was closed, or trimmed
// away by newer runs, or its game has gone without the watcher closing it. A closed record drops its
// pid, so the run is found by when it started.
func runClosed(dir string, rec instance.Launch) bool {
	records := instance.LoadLaunches(dir)
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].StartedAt == rec.StartedAt {
			return records[i].EndedAt != "" || (rec.PID != 0 && !proc.IsAlive(rec.PID))
		}
	}
	return true
}

// lastLines is the log's lines, only the last limit of them when limit is set.
func lastLines(text string, limit int) []string {
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	if text == "" {
		lines = nil
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}
