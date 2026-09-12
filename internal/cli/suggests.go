package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
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
			return a.printer.Emit(res, func(l *out.Lines) {
				if len(res.Suggestions) == 0 {
					l.Info("No suggestions.")
				}
				var items []out.Item
				for _, s := range res.Suggestions {
					aside := s.Kind + " " + s.On
					if s.Declared != "" && s.Declared != "*" {
						aside += " " + s.Declared
					}
					items = append(items, out.Item{Kind: out.Note, Name: s.Mod, Aside: []string{aside}})
				}
				l.Items(items...)
				if hidden > 0 {
					optionalNudge(l, hidden)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&optional, "optional", false, "also list optional integrations")
	return cmd
}

func optionalNudge(l *out.Lines, n int) {
	noun := "integrations"
	if n == 1 {
		noun = "integration"
	}
	l.Nudge(fmt.Sprintf("%d optional %s to see", n, noun), "shulker suggests --optional")
}
