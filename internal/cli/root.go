package cli

import (
	"io"

	"github.com/andrewmast/shulker/internal/out"
	"github.com/spf13/cobra"
)

var version = "dev"

type app struct {
	printer *out.Printer
}

func Execute(args []string, stdout, stderr io.Writer) int {
	a := &app{printer: &out.Printer{Stdout: stdout, Stderr: stderr, JSON: jsonRequested(args)}}
	root := a.root()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
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
			a.printer.Command = cmd.Name()
		},
	}
	root.PersistentFlags().BoolVar(&a.printer.JSON, "json", a.printer.JSON, "print machine-readable JSON, including errors")
	root.AddCommand(a.versionCmd())
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
