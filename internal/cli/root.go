package cli

import (
	"io"
	"os"
	"strings"

	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/pack"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var version = "dev"

type app struct {
	printer *out.Printer
	stdin   io.Reader
	tty     func() bool
	dir     string
	d       *deps
	packs   []*pack.Loaded
}

func Execute(args []string, stdout, stderr io.Writer) int {
	return newApp(stdout, stderr).run(args)
}

func newApp(stdout, stderr io.Writer) *app {
	return &app{printer: &out.Printer{Stdout: stdout, Stderr: stderr}, stdin: os.Stdin, tty: stdinIsTerminal}
}

func (a *app) run(args []string) int {
	a.printer.JSON = jsonRequested(args)
	root := a.root()
	root.SetArgs(args)
	root.SetOut(a.printer.Stdout)
	root.SetErr(a.printer.Stderr)
	if err := root.Execute(); err != nil {
		return a.printer.Fail(err)
	}
	return out.ExitOK
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
	root.PersistentFlags().BoolVar(&a.printer.JSON, "json", a.printer.JSON, "print machine-readable JSON, including errors")
	root.PersistentFlags().StringVarP(&a.dir, "dir", "C", a.dir, "project directory (default: current directory)")
	root.AddCommand(a.versionCmd(), a.initCmd(), a.addCmd(), a.removeCmd(), a.updateCmd(), a.outdatedCmd(), a.pinCmd(), a.unpinCmd(), a.installCmd(), a.buildCmd(), a.syncCmd(), a.serveCmd(), a.linkCmd(), a.exportCmd(), a.packCmd(), a.playerCmd())
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
