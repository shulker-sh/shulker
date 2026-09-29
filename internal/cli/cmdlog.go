package cli

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"shulker.sh/shulker/internal/cmdlog"
	"shulker.sh/shulker/internal/config"
)

// logMode is the annotation every runnable command declares what its runs do with, which decides
// what the log keeps of them.
const logMode = "log"

const (
	// logReads is a command that changes no file and nothing on the machine. A clean run of it
	// writes nothing to the log, and one that goes wrong writes everything but its result.
	logReads = "reads"
	// logActs is a command that writes a file or starts the game, logged in full even when it
	// turns out to have nothing to do.
	logActs = "acts"
	// logDecides is a command that reads or acts by its arguments. It is logged as reading until
	// it calls a.logActing.
	logDecides = "decides"
	// logNever is `shulker log`, which reads the log and never adds to it.
	logNever = "never"
)

func reads() map[string]string   { return map[string]string{logMode: logReads} }
func acts() map[string]string    { return map[string]string{logMode: logActs} }
func decides() map[string]string { return map[string]string{logMode: logDecides} }

type logState int

const (
	logOpen logState = iota
	logStarted
	logOff
)

// openLog readies the run's entries in log.jsonl beside config.json. It needs only the folder, so a
// config or registry that can't be read is still logged.
func (a *app) openLog(args []string) {
	path, err := config.Path()
	if err == nil {
		path = filepath.Join(filepath.Dir(path), cmdlog.FileName)
	}
	a.log = cmdlog.New(path, args)
	a.log.OnFail = func(err error) {
		// A hook's output lands in a launcher, which shows any of it as though the launch had
		// gone wrong.
		if isHook(a.log.Cmd) {
			return
		}
		a.printer.Warn("can't write shulker's log, so this run goes unrecorded: %v.", err)
	}
	a.printer.Recorder = a.log
}

// startLog opens the run's entry once, with its flags as parsed: cobra ends flags at "--", so
// nothing after it reaches here.
func (a *app) startLog(cmd *cobra.Command) {
	if a.log == nil || a.logState != logOpen {
		return
	}
	mode := cmd.Annotations[logMode]
	// The shell runs these on every tab press; they are its questions, not something shulker did.
	if name := cmd.Name(); mode == logNever || name == cobra.ShellCompRequestCmd || name == cobra.ShellCompNoDescRequestCmd {
		a.logState = logOff
		a.printer.Recorder = nil
		// What `shulker log` shows is what log.keepDays keeps, though it writes nothing itself.
		if mode == logNever {
			a.trimLog()
		}
		return
	}
	a.logState = logStarted
	// Cobra's own help and its errors, like an unknown command, change nothing either.
	a.log.ReadOnly = mode != logActs
	a.log.Opened = a.trimLog
	flags := map[string]string{}
	cmd.Flags().Visit(func(f *pflag.Flag) { flags[f.Name] = f.Value.String() })
	a.log.Start(strings.TrimPrefix(strings.TrimPrefix(cmd.CommandPath(), "shulker"), " "), commandGroup(cmd), cmp.Or(a.instance, a.dir), flags)
}

// logActing tells the log a logDecides command is going to change something.
func (a *app) logActing() {
	if a.logState == logStarted {
		a.log.Acts()
	}
}

// trimLog drops the entries past log.keepDays.
func (a *app) trimLog() {
	err := cmdlog.Trim(a.log.Path, configuredKeepDays(), a.log.Now())
	if err == nil {
		return
	}
	msg := fmt.Sprintf("can't trim shulker's log, so it keeps entries past log.keepDays: %v", err)
	// A hook's output lands in a launcher, so the log alone hears it there.
	if isHook(a.log.Cmd) {
		a.log.Warn(msg)
		return
	}
	a.printer.Warn("%s", msg)
}

// configuredKeepDays is log.keepDays, or its default when config.json can't be read: the run reports
// that itself, and its log still trims.
func configuredKeepDays() int {
	path, err := config.Path()
	if err != nil {
		return config.DefaultLogKeepDays
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return config.DefaultLogKeepDays
	}
	return cfg.Log.Days()
}

func (a *app) endLog(exit int) {
	if a.logState == logStarted {
		a.log.End(exit)
	}
}

// logInstance names the instance the run turned out to act on, for the entries still to come.
func (a *app) logInstance(id string) {
	if a.log != nil && id != "" {
		a.log.Instance = id
	}
}

// commandGroup is the help group of cmd's root command. The root itself is shulker's own.
func commandGroup(cmd *cobra.Command) string {
	for cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	if !cmd.HasParent() {
		return "shulker"
	}
	return helpGroupOf(cmd.Name())
}

func isHook(cmd string) bool {
	return cmd == "hook" || strings.HasPrefix(cmd, "hook ")
}
