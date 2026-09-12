package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/selfupdate"
	"shulker.sh/shulker/internal/server"
)

var version = devVersion

const helpFooter = `
Docs: https://shulker.sh/docs
For agents: https://shulker.sh/llms.txt
`

type app struct {
	printer    *out.Printer
	stdin      io.Reader
	tty        func() bool
	dir        string
	d          *deps
	configPath string
	packs      []*pack.Loaded
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
	a.printer.JSON = jsonRequested(args)
	root := a.root()
	root.SetArgs(args)
	root.SetOut(a.printer.Stdout)
	root.SetErr(a.printer.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		if !a.running && out.CodeOf(err) == "" {
			err = out.Errorf("usage", "%s", err)
		}
		if ctx.Err() != nil {
			err = &out.Error{Code: "interrupted", Message: "interrupted", Exit: out.ExitInterrupted, Data: out.AsError(err).Data}
		}
		return a.printer.Fail(err)
	}
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
		Use:           "shulker",
		Short:         "Manage Minecraft mods, client instances, and servers",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			a.printer.Command = strings.TrimPrefix(cmd.CommandPath(), "shulker ")
		},
	}
	root.SetHelpTemplate(root.HelpTemplate() + helpFooter)
	root.PersistentFlags().BoolVar(&a.printer.JSON, "json", a.printer.JSON, "print machine-readable JSON, including errors")
	root.PersistentFlags().StringVarP(&a.dir, "dir", "C", a.dir, "project directory (default: current directory)")
	root.AddCommand(a.versionCmd(), a.initCmd(), a.addCmd(), a.removeCmd(), a.lockCmd(), a.updateCmd(), a.outdatedCmd(), a.suggestsCmd(), a.pinCmd(), a.unpinCmd(), a.installCmd(), a.buildCmd(), a.diffCmd(), a.pullCmd(), a.syncCmd(), a.serveCmd(), a.linkCmd(), a.linksCmd(), a.unlinkCmd(), a.exportCmd(), a.importCmd(), a.packCmd(), a.targetCmd(), a.featureCmd(), a.playerCmd(), a.selfCmd())
	a.markRunning(root)
	return root
}

// Cobra reports an unknown command before parsing any flags, so the
// persistent flag cannot be trusted on that path.
func jsonRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
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
