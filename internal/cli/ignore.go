package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

type ignoreResult struct {
	manifest.Ignore
	Replaced bool `json:"replaced"`
}

func (a *app) ignoreCmd() *cobra.Command {
	var note, rule, declared string
	var force bool
	cmd := &cobra.Command{
		Use:   "ignore <mod> <on>",
		Short: "Record that a dependency problem between two mods is safe to ignore",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mod, on := args[0], args[1]
			if note == "" {
				return out.Errorf("usage", "--note is required: say why %s on %s is safe to ignore", mod, on)
			}
			if rule != "" && rule != "depends" && rule != "breaks" {
				return out.Errorf("usage", "--rule takes depends or breaks, not %q", rule)
			}
			if declared != "" && rule == "" {
				return out.Errorf("usage", "--declared needs --rule depends or --rule breaks")
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			existing := slices.IndexFunc(p.Manifest.Ignore, func(ig manifest.Ignore) bool { return ig.Mod == mod && ig.On == on })
			if existing >= 0 && !force {
				ig := p.Manifest.Ignore[existing]
				e := out.Errorf("already-ignored", "%s on %s is already ignored (%s %s); pass --force to replace it", mod, on, ig.Rule, ig.Declared)
				e.Flag = "--force"
				return e
			}
			entry := manifest.Ignore{Rule: rule, Mod: mod, On: on, Declared: declared, Note: note}
			if declared == "" {
				if err := a.requireLock(p); err != nil {
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
				problem, err := matchProblem(v.Problems, mod, on, rule)
				if err != nil {
					return err
				}
				switch {
				case problem != nil:
					entry.Rule, entry.Declared = problem.Rule, problem.Declared
				case existing >= 0:
					kept := p.Manifest.Ignore[existing]
					entry.Rule, entry.Declared = kept.Rule, kept.Declared
				default:
					return noProblem(v.Problems, mod, on)
				}
			}
			if existing >= 0 {
				p.Manifest.Ignore[existing] = entry
			} else {
				p.Manifest.Ignore = append(p.Manifest.Ignore, entry)
			}
			if err := p.SaveManifest(); err != nil {
				return err
			}
			res := ignoreResult{Ignore: entry, Replaced: existing >= 0}
			return a.printer.Emit(res, func(l *out.Lines) {
				verb := "ignored"
				if res.Replaced {
					verb = "replaced ignore for"
				}
				l.OK(fmt.Sprintf("%s %s on %s", verb, mod, on), entry.Rule+" "+entry.Declared)
			})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "why the constraint is safe to ignore (required)")
	cmd.Flags().StringVar(&rule, "rule", "", "the problem's rule, depends or breaks (printed with the problem)")
	cmd.Flags().StringVar(&declared, "declared", "", "the range the jar declares (printed with the problem); with it, nothing is resolved")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing ignore for the pair")
	return cmd
}

func matchProblem(problems []resolve.Problem, mod, on, rule string) (*resolve.Problem, error) {
	var matches []resolve.Problem
	for _, p := range problems {
		if p.Mod == mod && p.On == on && (rule == "" || p.Rule == rule) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	}
	return nil, out.Errorf("usage", "%s on %s has both a depends and a breaks problem; pass --rule depends or --rule breaks", mod, on)
}

func noProblem(problems []resolve.Problem, mod, on string) error {
	var pairs, ons []string
	for _, p := range problems {
		pairs = append(pairs, p.Mod+" "+p.On)
		if p.Mod == mod {
			ons = append(ons, p.On)
		}
	}
	if len(problems) == 0 {
		return out.Errorf("no-problem", "the locked mods have no problem between %s and %s; pass --rule and --declared from the failed command", mod, on)
	}
	e := out.Errorf("no-problem", "the locked mods have no problem between %s and %s; pass --rule and --declared from the failed command, or pick a current problem", mod, on)
	e.Candidates = pairs
	if len(ons) > 0 {
		e.Candidates, e.Given = ons, on
	}
	return e
}

func (a *app) unignoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unignore <mod> <on>",
		Short: "Drop an ignored dependency problem so it is checked again",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mod, on := args[0], args[1]
			p, err := a.openProject()
			if err != nil {
				return err
			}
			at := slices.IndexFunc(p.Manifest.Ignore, func(ig manifest.Ignore) bool { return ig.Mod == mod && ig.On == on })
			if at < 0 {
				e := out.Errorf("not-ignored", "%s on %s is not ignored in shulker.json", mod, on)
				for _, ig := range p.Manifest.Ignore {
					e.Candidates = append(e.Candidates, ig.Mod+" "+ig.On)
				}
				return e
			}
			entry := p.Manifest.Ignore[at]
			p.Manifest.Ignore = slices.Delete(p.Manifest.Ignore, at, at+1)
			if err := p.SaveManifest(); err != nil {
				return err
			}
			return a.printer.Emit(entry, func(l *out.Lines) {
				l.OK(fmt.Sprintf("unignored %s on %s", mod, on), strings.TrimSpace(entry.Rule+" "+entry.Declared))
			})
		},
	}
}
