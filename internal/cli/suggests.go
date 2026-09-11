package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/resolve"
)

type suggestions struct {
	Suggestions []resolve.Suggestion `json:"suggestions"`
}

func (a *app) suggestsCmd() *cobra.Command {
	var optional bool
	cmd := &cobra.Command{
		Use:   "suggests",
		Short: "List mods that locked mods recommend or suggest and that aren't installed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			r, err := a.resolver(cmd.Context(), p)
			if err != nil {
				return err
			}
			v, err := r.Validate()
			if err != nil {
				return err
			}
			a.warn(v.Warnings)
			res := suggestions{Suggestions: []resolve.Suggestion{}}
			hidden := 0
			for _, s := range v.Suggestions {
				if s.Kind == "optional" && !optional {
					hidden++
					continue
				}
				res.Suggestions = append(res.Suggestions, s)
			}
			return a.printer.Emit(res, func(w io.Writer) {
				if len(res.Suggestions) == 0 {
					fmt.Fprintln(w, "No suggestions.")
				}
				mod := ""
				for _, s := range res.Suggestions {
					if s.Mod != mod {
						fmt.Fprintln(w, s.Mod)
						mod = s.Mod
					}
					fmt.Fprintf(w, "  %s %s", s.Kind, s.On)
					if s.Declared != "" && s.Declared != "*" {
						fmt.Fprintf(w, " %s", s.Declared)
					}
					fmt.Fprintln(w)
				}
				if hidden > 0 {
					fmt.Fprintln(w, optionalHint(hidden))
				}
			})
		},
	}
	cmd.Flags().BoolVar(&optional, "optional", false, "also list optional integrations")
	return cmd
}

func optionalHint(n int) string {
	noun := "integrations"
	if n == 1 {
		noun = "integration"
	}
	return fmt.Sprintf("%d optional %s; see shulker suggests --optional", n, noun)
}
