package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
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
		Use:   "pre-launch",
		Short: "Sync the instance before the launcher starts the game",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, f, ok := a.hookInstance()
			if !ok || !f.Settings.PreLaunch() {
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
	cmd.Flags().DurationVar(&deadline, "deadline", 0, "stop the update after this long and explain why (default: no deadline)")
	return cmd
}

func (a *app) hookPostExitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-exit",
		Short: "Record how the run ended after the game exits",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, f, ok := a.hookInstance()
			if !ok || !f.Settings.PostExit() {
				return nil
			}
			a.closeLaunch(dir, f.Settings)
			return nil
		},
	}
}

// hookWrapCmd stands in for Java where the launcher has no command slots: the Mojang shim names the
// instance with -C and hands the game's own argv over after --. That argv carries the session access
// token, so it goes to Java and nowhere else: no warning, no record and no output repeats it.
func (a *app) hookWrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wrap -- <java arguments>",
		Short: "Sync the instance, then run the game with this machine's Java",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, argv []string) error {
			// The launcher's version check runs the shim without --gameDir, and that call just
			// wants Java.
			gameDir, launching := gameDirOf(argv)
			if a.dir == "" && a.instance == "" {
				a.dir = gameDir
			}
			dir, f, ok := a.hookInstance()
			if !ok {
				return nil
			}
			if launching && f.Settings.PreLaunch() {
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
			java := f.Settings.Java
			if java == "" && f.Resolved != nil {
				java = f.Resolved.Java
			}
			if java == "" {
				a.printer.Warn("%s records no Java to run the game with; run shulker instances repair", instance.Path(dir))
				return nil
			}
			exe, args := java, argv
			if w := f.Settings.Wrapper; len(w) > 0 {
				exe = w[0]
				args = append(append(append([]string{}, w[1:]...), java), argv...)
			}
			gameOut := a.printer.Stdout
			if a.printer.JSON {
				gameOut = a.printer.Stderr
			}
			game := exec.Command(exe, args...)
			game.Stdin, game.Stdout, game.Stderr = a.stdin, gameOut, a.printer.Stderr
			code := 0
			if err := game.Run(); err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					a.printer.Warn("run %s: %v", exe, err)
					return nil
				}
				code = exit.ExitCode()
			}
			if launching && f.Settings.PostExit() {
				a.closeLaunch(dir, f.Settings)
			}
			if code != 0 {
				return &out.Error{Code: "game-exit", Message: fmt.Sprintf("game exited with status %d", code), Exit: code}
			}
			return nil
		},
	}
}

func gameDirOf(argv []string) (string, bool) {
	for i, arg := range argv {
		if arg == "--gameDir" && i+1 < len(argv) {
			return argv[i+1], true
		}
		if v, ok := strings.CutPrefix(arg, "--gameDir="); ok {
			return v, true
		}
	}
	return "", false
}

func (a *app) syncForLaunch(cmd *cobra.Command, dir string) (syncResult, error) {
	p, side, inPlace, err := a.inPlaceProject(dir)
	switch {
	case err != nil:
		return syncResult{}, err
	case inPlace:
		return a.syncInPlaceForLaunch(cmd, p, side)
	default:
		return a.syncRecorded(cmd, syncRequest{into: dir})
	}
}

// closeLaunch ends the record stampLaunch opened. The outcome is read from a crash report newer
// than the start, since no launcher slot passes the exit code on.
func (a *app) closeLaunch(dir string, s instance.Settings) {
	keep := s.LaunchKeep()
	if keep == 0 {
		return
	}
	records := instance.LoadLaunches(dir)
	open := -1
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].EndedAt == "" {
			open = i
			break
		}
	}
	if open < 0 {
		return
	}
	started, err := time.Parse(time.RFC3339, records[open].StartedAt)
	if err != nil {
		started = time.Time{}
	}
	log, crash := serverFailureFiles(dir, started)
	records[open].EndedAt = time.Now().UTC().Format(time.RFC3339)
	records[open].Outcome = instance.OutcomeOK
	records[open].Log = log
	if crash != "" {
		records[open].Outcome = instance.OutcomeCrashed
		records[open].CrashReport = crash
	}
	if err := instance.SaveLaunches(dir, records, keep); err != nil {
		a.printer.Warn("%v", err)
	}
}

// hookInstance is the directory the script named and the intent it records. A hook never fails a
// launch, so anything unreadable is a warning and nothing to do.
func (a *app) hookInstance() (string, *instance.File, bool) {
	dir, err := a.scopeDir()
	if err != nil {
		a.printer.Warn("%v", err)
		return "", nil, false
	}
	f, err := instance.Load(dir)
	if err != nil {
		a.printer.Warn("%v", err)
		return "", nil, false
	}
	return dir, f, true
}

// stampLaunch opens a launch record for the run about to start; post-exit closes it. A run whose
// post-exit never fires stays open, which is how an abandoned run reads.
func (a *app) stampLaunch(dir string, s instance.Settings) {
	keep := s.LaunchKeep()
	if keep == 0 {
		return
	}
	records := append(instance.LoadLaunches(dir), instance.Launch{StartedAt: time.Now().UTC().Format(time.RFC3339)})
	if err := instance.SaveLaunches(dir, records, keep); err != nil {
		a.printer.Warn("%v", err)
	}
}

// instanceID is the id `-i` takes for a directory, for the message that names it. Empty when the
// registry can't be read, which only costs the message its command.
func (a *app) instanceID(dir string) string {
	in, _ := a.registeredInstance(dir)
	return in.ID
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
	e.Plain = true
	if id != "" {
		e.Nudge = out.Nudge{Lead: "Launch again to resume it, or finish the download first with", Command: "shulker sync -i " + id}
	}
	return e
}

func humanMinutes(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		if m := int(d / time.Minute); m == 1 {
			return "1 minute"
		} else {
			return fmt.Sprintf("%d minutes", m)
		}
	}
	return d.String()
}
