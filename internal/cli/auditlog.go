package cli

import (
	"cmp"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"shulker.sh/shulker/internal/auditlog"
	"shulker.sh/shulker/internal/config"
)

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
		path = filepath.Join(filepath.Dir(path), auditlog.FileName)
	}
	a.log = auditlog.New(path, args)
	a.log.OnFail = func(err error) {
		// A hook's output lands in a launcher, which shows any of it as though the launch had
		// gone wrong.
		if isHook(a.log.Cmd) {
			return
		}
		a.printer.Warn("can't write shulker's log, so this run goes unrecorded: %v", err)
	}
	a.printer.Recorder = a.log
}

// startLog opens the run's entry once, with its flags as parsed: cobra ends flags at "--", so
// nothing after it reaches here.
func (a *app) startLog(cmd *cobra.Command) {
	if a.log == nil || a.logState != logOpen {
		return
	}
	// The shell runs these on every tab press; they are its questions, not something shulker did.
	if name := cmd.Name(); name == cobra.ShellCompRequestCmd || name == cobra.ShellCompNoDescRequestCmd {
		a.logState = logOff
		a.printer.Recorder = nil
		return
	}
	a.logState = logStarted
	flags := map[string]string{}
	cmd.Flags().Visit(func(f *pflag.Flag) { flags[f.Name] = f.Value.String() })
	a.log.Start(strings.TrimPrefix(strings.TrimPrefix(cmd.CommandPath(), "shulker"), " "), commandGroup(cmd), cmp.Or(a.instance, a.dir), flags)
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
