// Package cli is shulker's command line: the cobra commands, their flags and what they print.
package cli

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
	"shulker.sh/shulker/internal/cmdlog"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/play"
	"shulker.sh/shulker/internal/saves"
	"shulker.sh/shulker/internal/selfupdate"
	"shulker.sh/shulker/internal/server"
	"shulker.sh/shulker/internal/sync"
)

const agentHelp = `Scripts and agents: pass --json. Every command then prints one JSON object on
stdout, errors included. Act on error.code rather than the message, and run
"shulker lock" when lockStale is true. To investigate what a project runs, start
with "shulker security" and read "shulker docs security".`

type app struct {
	printer     *out.Printer
	style       out.Options
	stdin       io.Reader
	tty         func() bool
	asker       asker
	dir         string
	instance    string
	d           *deps
	se          *sync.Env
	pe          *play.Env
	configPath  string
	home        string
	releases    *selfupdate.Releases
	build       func() selfupdate.Build
	exe         func() (string, error)
	installer   func(ctx context.Context, java, jar string, args []string) error
	watcher     func(req game.Launch) (int, error)
	openURL     func(url string) error
	isRunning   bool
	backedUp    map[saves.Home]bool
	log         *cmdlog.Log
	logState    logState
	failFast    bool
	everyFetch  bool
	yes         bool
	warnsRawURL bool
}

// Execute runs shulker with args and returns the process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	// A copy of shulker installed as an instance's javaw.exe is a launcher's Java, not the CLI: it
	// hands the launch on and never parses these arguments, which are the game's.
	if launcher.IsShim() {
		return launcher.RunShim()
	}
	if runtime.GOOS == "windows" {
		selfupdate.RemoveOld()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal cancels ctx, a second one ends the process at once.
	context.AfterFunc(ctx, stop)
	return newApp(stdout, stderr).run(ctx, args)
}

func newApp(stdout, stderr io.Writer) *app {
	return &app{printer: &out.Printer{Stdout: stdout, Stderr: stderr}, stdin: os.Stdin, tty: stdinIsTerminal, build: describeBuild, exe: selfupdate.Executable, installer: server.RunInstaller, openURL: openURL}
}

func (a *app) run(ctx context.Context, args []string) int {
	a.printer.JSON = isFlagRequested(args, "json")
	a.printer.NoInput = isFlagRequested(args, "no-input")
	a.printer.Annotate = annotates(args)
	a.printer.Args = args
	a.style = out.Options{NoColor: isFlagRequested(args, "no-color"), ASCII: isFlagRequested(args, "ascii")}
	if !a.printer.JSON {
		a.printer.Theme, a.printer.ErrTheme = out.Detect(a.printer.Stdout, a.printer.Stderr, a.style)
	}
	a.openLog(args)
	root := a.root()
	root.SetArgs(args)
	root.SetOut(a.printer.Stdout)
	root.SetErr(a.printer.Stderr)
	cmd, err := root.ExecuteContextC(ctx)
	a.startLog(cmp.Or(cmd, root))
	code := out.ExitOK
	if err != nil {
		if !a.isRunning && out.CodeOf(err) == "" {
			err = usageError(root, err)
		}
		if out.CodeOf(err) == "usage" && cmd != nil {
			e := out.AsError(err)
			e.Usage = helpUsageText(cmd)
			if cmd.HasParent() {
				e.UsageCommand = cmd.CommandPath()
			}
		}
		if e := out.AsError(err); e.Flag != "" && cmd != nil && cmd.Flags().Lookup(strings.TrimPrefix(e.Flag, "--")) == nil {
			e.Flag = ""
		}
		if a.printer.Command == "" && cmd != nil && cmd.HasParent() {
			a.printer.Command = strings.TrimPrefix(cmd.CommandPath(), "shulker ")
		}
		if ctx.Err() != nil {
			err = &out.Error{Code: "interrupted", Message: "interrupted", Exit: out.ExitInterrupted, Data: out.AsError(err).Data}
		}
		code = a.printer.Fail(err)
	} else {
		a.printer.Finish()
	}
	a.endLog(code)
	return code
}

// markRunning tells cobra's own errors (unknown command or flag, bad
// arguments, flag groups) apart from errors a command returns.
func (a *app) markRunning(c *cobra.Command) {
	if run := c.RunE; run != nil {
		c.RunE = func(cmd *cobra.Command, args []string) error {
			a.isRunning = true
			return run(cmd, args)
		}
	}
	for _, sub := range c.Commands() {
		a.markRunning(sub)
	}
}

func (a *app) root() *cobra.Command {
	root := &cobra.Command{
		Use:                "shulker",
		Short:              "Manage Minecraft mods, client instances, and servers",
		Long:               agentHelp,
		SilenceUsage:       true,
		SilenceErrors:      true,
		DisableSuggestions: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			a.printer.Command = strings.TrimPrefix(cmd.CommandPath(), "shulker ")
			a.startLog(cmd)
		},
	}
	// These values are read from the arguments before cobra parses them, so
	// they are put back after the flags are defined with the zero defaults
	// help prints.
	jsonOut, noInput, noColor, ascii := a.printer.JSON, a.printer.NoInput, a.style.NoColor, a.style.ASCII
	root.PersistentFlags().BoolVar(&a.printer.JSON, "json", false, "print machine-readable JSON, including errors.")
	root.PersistentFlags().BoolVar(&a.printer.NoInput, "no-input", false, "ask nothing: take every default, and fail on a missing required value.")
	// --id is the same flag: the row calls it id, and the flag says which instance.
	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "id" {
			name = "instance"
		}
		return pflag.NormalizedName(name)
	})
	root.PersistentFlags().BoolVar(&a.style.NoColor, "no-color", false, "print without colour (NO_COLOR does the same).")
	root.PersistentFlags().BoolVar(&a.style.ASCII, "ascii", false, "print with ASCII glyphs instead of ✔ ✘ ├─ ⟶ ».")
	root.PersistentFlags().Bool("annotations", false, "also print errors and warnings as GitHub Actions annotations (the default when GITHUB_ACTIONS=true).")
	root.PersistentFlags().Bool("no-annotations", false, "print no GitHub Actions annotations, even when GITHUB_ACTIONS=true.")
	a.printer.JSON, a.printer.NoInput, a.style.NoColor, a.style.ASCII = jsonOut, noInput, noColor, ascii
	root.AddCommand(a.versionCmd(), a.initCmd(), a.createCmd(), a.addCmd(), a.searchCmd(), a.removeCmd(), a.listCmd(), a.lockCmd(), a.checkCmd(), a.auditCmd(), a.matchCmd(), a.updateCmd(), a.outdatedCmd(), a.suggestsCmd(), a.pinCmd(), a.unpinCmd(), a.ignoreCmd(), a.unignoreCmd(), a.installCmd(), a.buildCmd(), a.diffCmd(), a.pullCmd(), a.syncCmd(), a.serveCmd(), a.linkCmd(), a.instancesCmd(), a.instanceCmd(), a.unlinkCmd(), a.exportCmd(), a.importCmd(), a.historyCmd(), a.rollbackCmd(), a.backupCmd(), a.restoreCmd(), a.savesCmd(), a.setCmd(), a.unsetCmd(), a.getCmd(), a.configCmd(), a.featureCmd(), a.playerCmd(), a.accountsCmd(), a.playCmd(), a.watchCmd(), a.selfCmd(), a.docsCmd(), a.cacheCmd(), a.securityCmd(), a.logCmd(), a.completionCmd())
	root.AddCommand(a.typeGroupCmds()...)
	// hook is hidden: the generated scripts run it, nobody types it.
	root.AddCommand(a.hookCmd())
	root.CompletionOptions.DisableDefaultCmd = true
	groupCommands(root)
	a.installHelp(root)
	a.markRunning(root)
	return root
}

// dirFlag gives c -C, for a command that reads a.dir. The path has to name a directory, which is
// checked as the flag is parsed.
func (a *app) dirFlag(c *cobra.Command) {
	c.Flags().VarP(&existingDir{&a.dir}, "dir", "C", "project directory (default: current directory)")
}

// uncheckedDirFlag gives c a -C that may name a directory that doesn't exist, for a command that
// creates the project there or that reports a missing directory itself. Binding the flag resets a.dir to its empty default, so a directory
// set before the commands are built is put back.
func (a *app) uncheckedDirFlag(c *cobra.Command) {
	dir := a.dir
	c.Flags().StringVarP(&a.dir, "dir", "C", "", "project directory (default: current directory)")
	a.dir = dir
}

// existingDir is a -C value that has to name a directory. An empty one is the current directory,
// as it is for git -C.
type existingDir struct{ dir *string }

func (d *existingDir) Set(path string) error {
	info, err := os.Stat(path)
	switch {
	case path == "":
	case errors.Is(err, fs.ErrNotExist):
		return flagReason("names " + path + ", which doesn't exist")
	case err != nil:
		return flagReason("names " + path + ", which can't be read")
	case !info.IsDir():
		return flagReason("names " + path + ", which is a file")
	}
	*d.dir = path
	return nil
}

func (d *existingDir) String() string { return *d.dir }
func (d *existingDir) Type() string   { return "string" }

// instanceFlag gives c -i, for a command that reads a.instance. The root's normalization lets it be
// spelled --id too.
func (a *app) instanceFlag(c *cobra.Command) {
	instance := a.instance
	c.Flags().StringVarP(&a.instance, "instance", "i", "", "act on a registered instance, by id, name, or directory.")
	a.instance = instance
}

// scopeFlags gives c both -C and -i, for a command that acts on a project or instance directory.
func (a *app) scopeFlags(c *cobra.Command) {
	a.dirFlag(c)
	a.instanceFlag(c)
}

// groupCommands gives every command that only groups others an action: its
// help when called alone, otherwise the unknown subcommand as a usage error
// with picks, the way the root reports one. Cobra would print the help and exit
// 0 for both. The group takes any arguments so the action sees the typo.
func groupCommands(c *cobra.Command) {
	for _, sub := range c.Commands() {
		if sub.HasSubCommands() && sub.Run == nil && sub.RunE == nil {
			sub.Args = nil
			sub.Annotations = reads()
			sub.RunE = func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					return cmd.Help()
				}
				return unknownSubcommand(cmd, args[0])
			}
		}
		groupCommands(sub)
	}
}

// unknownSubcommand is the usage error for a word that names none of parent's
// commands, with those commands as picks.
func unknownSubcommand(parent *cobra.Command, word string) *out.Error {
	e := out.Errorf("usage", "unknown command %q for %q", word, parent.CommandPath())
	if !parent.HasParent() {
		e = out.Errorf("usage", "unknown command %q", word)
	}
	for _, c := range parent.Commands() {
		if c.IsAvailableCommand() {
			e.Candidates = append(e.Candidates, c.Name())
		}
	}
	e.Given = word
	return e
}

var unknownCommand = regexp.MustCompile(`^unknown command "([^"]+)" for "([^"]+)"`)

// usageError turns cobra's own error into the usage error. An unknown command carries the
// parent's subcommands as candidates, so it gets the same picks and example as any other typo.
func usageError(root *cobra.Command, err error) error {
	if e, ok := flagError(err); ok {
		return e
	}
	e := &out.Error{Code: "usage", Message: err.Error(), Exit: out.ExitUsage}
	m := unknownCommand.FindStringSubmatch(err.Error())
	if m == nil {
		return e
	}
	parent := root
	if path := strings.Fields(m[2]); len(path) > 1 {
		found, _, findErr := root.Find(path[1:])
		if findErr != nil || found == nil {
			return e
		}
		parent = found
	}
	if parent == root {
		e.Message = `unknown command "` + m[1] + `"`
	}
	for _, c := range parent.Commands() {
		if c.IsAvailableCommand() {
			e.Candidates = append(e.Candidates, c.Name())
		}
	}
	e.Given = m[1]
	return e
}

// annotates reports whether errors and warnings also print as GitHub Actions annotations: in a
// workflow run unless --no-annotations says otherwise, and anywhere with --annotations.
func annotates(args []string) bool {
	if isFlagRequested(args, "no-annotations") {
		return false
	}
	return isFlagRequested(args, "annotations") || os.Getenv("GITHUB_ACTIONS") == "true"
}

// Cobra reports an unknown command before parsing any flags, so the
// persistent flags cannot be trusted on that path.
func isFlagRequested(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || arg == "--"+name+"=true" {
			return true
		}
		if arg == "--" {
			return false
		}
	}
	return false
}

func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
