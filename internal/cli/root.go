package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/selfupdate"
	"shulker.sh/shulker/internal/server"
)

var version = devVersion

const agentHelp = `Scripts and agents: pass --json. Every command then prints one JSON object on
stdout, errors included. Act on error.code rather than the message, and run
"shulker lock" when lockStale is true.`

type app struct {
	printer    *out.Printer
	style      out.Options
	stdin      io.Reader
	tty        func() bool
	dir        string
	instance   string
	d          *deps
	configPath string
	packs      []*pack.Loaded
	relocking  bool
	releases   *selfupdate.Releases
	exe        func() (string, error)
	installer  func(ctx context.Context, java, jar string, args []string) error
	running    bool
}

func Execute(args []string, stdout, stderr io.Writer) int {
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
	return &app{printer: &out.Printer{Stdout: stdout, Stderr: stderr}, stdin: os.Stdin, tty: stdinIsTerminal, exe: selfupdate.Executable, installer: server.RunInstaller}
}

func (a *app) run(ctx context.Context, args []string) int {
	a.printer.JSON = flagRequested(args, "json")
	a.printer.Args = args
	a.style = out.Options{NoColor: flagRequested(args, "no-color"), ASCII: flagRequested(args, "ascii")}
	if !a.printer.JSON {
		a.printer.Theme, a.printer.ErrTheme = out.Detect(a.printer.Stdout, a.printer.Stderr, a.style)
	}
	root := a.root()
	root.SetArgs(args)
	root.SetOut(a.printer.Stdout)
	root.SetErr(a.printer.Stderr)
	if cmd, err := root.ExecuteContextC(ctx); err != nil {
		if !a.running && out.CodeOf(err) == "" {
			err = usageError(root, err)
		}
		if out.CodeOf(err) == "usage" && cmd != nil {
			out.AsError(err).Usage = func(l *out.Lines) { helpUsage(l, cmd) }
		}
		if ctx.Err() != nil {
			err = &out.Error{Code: "interrupted", Message: "interrupted", Exit: out.ExitInterrupted, Data: out.AsError(err).Data}
		}
		return a.printer.Fail(err)
	}
	a.printer.Settle()
	return out.ExitOK
}

// markRunning tells cobra's own errors (unknown command or flag, bad
// arguments, flag groups) apart from errors a command returns.
func (a *app) markRunning(c *cobra.Command) {
	if run := c.RunE; run != nil {
		c.RunE = func(cmd *cobra.Command, args []string) error {
			a.running = true
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
		},
	}
	// These values are read from the arguments before cobra parses them, so
	// they are put back after the flags are defined with the zero defaults
	// help prints.
	jsonOut, dir, noColor, ascii := a.printer.JSON, a.dir, a.style.NoColor, a.style.ASCII
	root.PersistentFlags().BoolVar(&a.printer.JSON, "json", false, "print machine-readable JSON, including errors")
	root.PersistentFlags().StringVarP(&a.dir, "dir", "C", "", "project directory (default: current directory)")
	root.PersistentFlags().StringVarP(&a.instance, "instance", "i", "", "act on a registered instance, by id, name, or directory")
	// --id is the same flag: the row calls it id, and the flag says which instance.
	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "id" {
			name = "instance"
		}
		return pflag.NormalizedName(name)
	})
	root.PersistentFlags().BoolVar(&a.style.NoColor, "no-color", false, "print without colour (NO_COLOR does the same)")
	root.PersistentFlags().BoolVar(&a.style.ASCII, "ascii", false, "print with ASCII glyphs instead of ✔ ✘ ├─ ⟶ »")
	a.printer.JSON, a.dir, a.style.NoColor, a.style.ASCII = jsonOut, dir, noColor, ascii
	root.AddCommand(a.versionCmd(), a.initCmd(), a.addCmd(), a.removeCmd(), a.listCmd(), a.lockCmd(), a.updateCmd(), a.outdatedCmd(), a.suggestsCmd(), a.pinCmd(), a.unpinCmd(), a.ignoreCmd(), a.unignoreCmd(), a.installCmd(), a.buildCmd(), a.diffCmd(), a.pullCmd(), a.syncCmd(), a.serveCmd(), a.linkCmd(), a.instancesCmd(), a.unlinkCmd(), a.exportCmd(), a.importCmd(), a.historyCmd(), a.rollbackCmd(), a.setCmd(), a.unsetCmd(), a.getCmd(), a.configCmd(), a.featureCmd(), a.playerCmd(), a.selfCmd(), a.docsCmd(), a.cacheCmd(), a.completionCmd())
	root.AddCommand(a.typeGroupCmds()...)
	// hook is hidden and in no help group: the generated scripts run it, nobody types it.
	root.AddCommand(a.hookCmd())
	root.CompletionOptions.DisableDefaultCmd = true
	groupCommands(root)
	a.installHelp(root)
	a.markRunning(root)
	return root
}

// groupCommands gives every command that only groups others an action: its
// help when called alone, otherwise the unknown subcommand as a usage error
// with picks, the way the root reports one. Cobra would print the help and exit
// 0 for both. The group takes any arguments so the action sees the typo.
func groupCommands(c *cobra.Command) {
	for _, sub := range c.Commands() {
		if sub.HasSubCommands() && sub.Run == nil && sub.RunE == nil {
			sub.Args = nil
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
	e := out.Errorf("usage", "%s", err)
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

// Cobra reports an unknown command before parsing any flags, so the
// persistent flags cannot be trusted on that path.
func flagRequested(args []string, name string) bool {
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
