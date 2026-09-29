package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/sync"
)

// hookCmd is what the generated scripts run. It is hidden: nothing should be typed by hand here, and
// a pre-launch run outside a launcher slot would sync whatever directory it landed in.
func (a *app) hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Run the work a launcher's own command slots ask for",
		Hidden: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return unknownSubcommand(cmd, args[0])
			}
			return nil
		},
	}
	cmd.AddCommand(a.hookPreLaunchCmd(), a.hookPostExitCmd(), a.hookWrapCmd())
	return cmd
}

func (a *app) hookPreLaunchCmd() *cobra.Command {
	var deadline time.Duration
	cmd := &cobra.Command{
		Use:         "pre-launch",
		Annotations: acts(),
		Short:       "Sync the instance before the launcher starts the game",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, f, err := a.hookInstance()
			if err != nil {
				a.printer.Warn("%v", err)
				return nil
			}
			if !f.Settings.PreLaunch() {
				return nil
			}
			a.stampLaunch(dir, f.Settings)
			if deadline > 0 {
				ctx, cancel := context.WithTimeout(cmd.Context(), deadline)
				defer cancel()
				cmd.SetContext(ctx)
			}
			res, err := a.syncForLaunch(cmd, dir)
			if err == nil {
				return a.printer.Emit(res, res.print)
			}
			if deadline > 0 && errors.Is(cmd.Context().Err(), context.DeadlineExceeded) {
				return updatePaused(deadline, a.instanceID(dir))
			}
			// A failure inside shulker must never stop the game starting: the launcher plays what is
			// already on disk.
			a.printer.Warn("%v", err)
			return nil
		},
	}
	a.uncheckedDirFlag(cmd)
	a.instanceFlag(cmd)
	cmd.Flags().DurationVar(&deadline, "deadline", 0, "stop the update after this long and explain why (default: no deadline)")
	return cmd
}

func (a *app) hookPostExitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "post-exit",
		Annotations: acts(),
		Short:       "Record how the run ended after the game exits",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, f, err := a.hookInstance()
			if err != nil {
				a.printer.Warn("%v", err)
				return nil
			}
			if !f.Settings.PostExit() {
				return nil
			}
			if _, err := instance.CloseRun(dir, f.Settings.LaunchKeep(), 0, instance.NoExitCode); err != nil {
				a.printer.Warn("%v", err)
			}
			return nil
		},
	}
	a.uncheckedDirFlag(cmd)
	a.instanceFlag(cmd)
	return cmd
}

// hookWrapCmd stands in for Java where the launcher has no command slots: the Mojang shim names the
// instance with -C and hands the game's own argv over after --. That argv carries the session access
// token, so it goes to Java and nowhere else: no warning, no record and no output repeats it.
func (a *app) hookWrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "wrap -- <java arguments>",
		Annotations: acts(),
		Short:       "Sync the instance, then run the game with this machine's Java",
		Args:        cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, argv []string) error {
			// The launcher's version check runs the shim without --gameDir, and that call just
			// wants Java.
			gameDir, launching := game.GameDirOf(argv)
			if a.dir == "" && a.instance == "" {
				a.dir = gameDir
			}
			dir, f, err := a.hookInstance()
			if err != nil {
				return notStarted(err.Error())
			}
			stamped := launching && f.Settings.PreLaunch()
			if stamped {
				a.stampLaunch(dir, f.Settings)
				if res, err := a.syncForLaunch(cmd, dir); err != nil {
					a.printer.Warn("%v", err)
				} else if err := a.printer.Emit(res, res.print); err != nil {
					return err
				}
				if synced, err := instance.Load(dir); err == nil {
					f = synced
				}
			}
			java := f.Java()
			if java == "" {
				reason := instance.Path(dir) + " records no Java to run the game with"
				if launching {
					if err := instance.FailLaunch(dir, f.Settings.LaunchKeep(), stamped, reason); err != nil {
						a.printer.Warn("%v", err)
					}
				}
				e := notStarted(reason)
				e.Rows = []out.Detail{{Label: "Fix", Text: "shulker instances repair", IsCommand: true}}
				return e
			}
			code, gaveWay, err := game.Run(game.Launch{Java: java, Argv: argv, Wrapper: f.Settings.Wrapper}, a.stdin, a.gameStdout(), a.printer.Stderr)
			if gaveWay != nil {
				a.printer.Warn("can't run the wrapper %q, so the game starts with Java alone: %v.", f.Settings.Wrapper[0], gaveWay)
			}
			if err != nil {
				if launching {
					if err := instance.FailLaunch(dir, f.Settings.LaunchKeep(), stamped, runReason(java, err)); err != nil {
						a.printer.Warn("%v", err)
					}
				}
				return notStarted(fmt.Sprintf("can't run Java at %s, so the game didn't start: %v", java, err))
			}
			if launching && f.Settings.PostExit() {
				if _, err := instance.CloseRun(dir, f.Settings.LaunchKeep(), 0, instance.NoExitCode); err != nil {
					a.printer.Warn("%v", err)
				}
			}
			if code != 0 {
				return &out.Error{Code: "game-exit", Message: fmt.Sprintf("game exited with status %d", code), Exit: code}
			}
			return nil
		},
	}
	a.uncheckedDirFlag(cmd)
	a.instanceFlag(cmd)
	return cmd
}

// gameStdout is where a game run in the foreground writes: stdout, unless that is the JSON
// envelope's.
func (a *app) gameStdout() io.Writer {
	if a.printer.JSON {
		return a.printer.Stderr
	}
	return a.printer.Stdout
}

// runReason is why a program never started, as one sentence. The operating system repeats the path
// inside its own error, so the path it carries is unwrapped first.
func runReason(exe string, err error) string {
	var (
		path *fs.PathError
		look *exec.Error
	)
	switch {
	case errors.As(err, &path):
		err = path.Err
	case errors.As(err, &look):
		err = look.Err
	}
	return fmt.Sprintf("run %s: %v", exe, err)
}

func (a *app) syncForLaunch(cmd *cobra.Command, dir string) (syncResult, error) {
	se, err := a.syncEnv()
	if err != nil {
		return syncResult{}, err
	}
	res, err := sync.ForLaunch(cmd.Context(), se, dir, cmd.Name())
	return a.synced(res, syncRequest{Request: sync.Request{Into: dir}}, err)
}

// hookInstance is the directory the script named and the intent it records. Its callers decide what
// an unreadable one costs: a hook running beside a launch the launcher drives itself warns and
// leaves it alone, while `hook wrap` is the launch and has to say so.
func (a *app) hookInstance() (string, *instance.File, error) {
	dir, err := a.scopeDir()
	if err != nil {
		return "", nil, err
	}
	f, err := instance.Load(dir)
	if err != nil {
		return "", nil, err
	}
	a.logInstance(a.instanceID(dir))
	return dir, f, nil
}

// notStarted is a launch that never happened, which the Mojang launcher shows as an error of its own
// because a non-zero exit is all it reads from the shim. Shulker's own failures around a launch that
// is going ahead stay an exit of 0: a sync that failed, or a wrapper that gave way to Java alone.
func notStarted(reason string) *out.Error {
	return out.Errorf("launch-not-started", "%s", reason)
}

// stampLaunch opens a launch record for the run about to start; post-exit closes it. It records no
// pid, because the launcher started the game and shulker has no process to point at. A run whose
// post-exit never fires stays open, which is how an abandoned run reads.
func (a *app) stampLaunch(dir string, s instance.Settings) {
	if err := instance.OpenRun(dir, s.LaunchKeep(), instance.Launch{StartedAt: instance.NowStamp()}); err != nil {
		a.printer.Warn("%v", err)
	}
}

// instanceID is the id `-i` takes for a directory, for the message that names it. Empty when the
// registry can't be read, which only costs the message its command.
func (a *app) instanceID(dir string) string {
	se, err := a.syncEnv()
	if err != nil {
		return ""
	}
	return sync.InstanceID(se, dir)
}

// registeredInstance is the registry row for a directory, when there is one.
func (a *app) registeredInstance(dir string) (config.Instance, bool) {
	instances, err := a.loadInstances()
	if err != nil {
		return config.Instance{}, false
	}
	if i, ok := config.FindInstance(instances, dir); ok {
		return instances[i], true
	}
	return config.Instance{}, false
}

// updatePaused is shown by GDLauncher as the failed task's error, in a dialog rather than a
// terminal, so it prints without shulker's usual error decoration.
func updatePaused(after time.Duration, id string) *out.Error {
	e := out.Errorf("update-paused", "This pack's update took longer than %s, so shulker paused it.", humanMinutes(after))
	e.IsPlain = true
	if id != "" {
		e.Nudge = out.Nudge{Lead: "Launch again to resume it, or finish the download first with", Command: "shulker sync -i " + id}
	}
	return e
}

func humanMinutes(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		if m := int(d / time.Minute); m != 1 {
			return fmt.Sprintf("%d minutes", m)
		}
		return "1 minute"
	}
	return d.String()
}
