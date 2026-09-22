package cli

import (
	"io"

	"github.com/spf13/cobra"
)

func (a *app) completionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Print a shell completion script",
	}
	shells := []struct {
		name, title string
		write       func(root *cobra.Command, w io.Writer, descriptions bool) error
	}{
		{"bash", "bash", func(root *cobra.Command, w io.Writer, descriptions bool) error {
			return root.GenBashCompletionV2(w, descriptions)
		}},
		{"fish", "fish", func(root *cobra.Command, w io.Writer, descriptions bool) error {
			return root.GenFishCompletion(w, descriptions)
		}},
		{"powershell", "PowerShell", func(root *cobra.Command, w io.Writer, descriptions bool) error {
			if descriptions {
				return root.GenPowerShellCompletionWithDesc(w)
			}
			return root.GenPowerShellCompletion(w)
		}},
		{"zsh", "zsh", func(root *cobra.Command, w io.Writer, descriptions bool) error {
			if descriptions {
				return root.GenZshCompletion(w)
			}
			return root.GenZshCompletionNoDesc(w)
		}},
	}
	for _, shell := range shells {
		var noDescriptions bool
		sub := &cobra.Command{
			Use:               shell.name,
			Annotations:       reads(),
			Short:             "Print the " + shell.title + " completion script",
			Args:              noArgs,
			ValidArgsFunction: cobra.NoFileCompletions,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return shell.write(cmd.Root(), cmd.OutOrStdout(), !noDescriptions)
			},
		}
		sub.Flags().BoolVar(&noDescriptions, "no-descriptions", false, "leave command descriptions out of the completions")
		cmd.AddCommand(sub)
	}
	return cmd
}
