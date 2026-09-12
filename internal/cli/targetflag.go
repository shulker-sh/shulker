package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
)

// targetFlag is the target a command works on, named either positionally or
// with --target. Commands whose positional slot holds something else register
// only the flag.
type targetFlag struct {
	name string
}

func (t *targetFlag) register(cmd *cobra.Command, usage string) {
	cmd.Flags().StringVar(&t.name, "target", "", usage)
}

func (t *targetFlag) resolve(args []string) (string, error) {
	if len(args) == 0 {
		return t.name, nil
	}
	if t.name != "" {
		return "", out.Errorf("usage", "pass the target once, either as an argument or with --target")
	}
	return args[0], nil
}
