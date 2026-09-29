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
		Use:         "ignore <mod> <on>",
		Annotations: acts(),
		Short:       "Record that a dependency problem between two mods is safe to ignore",
		Args:        exactArgs(2),
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
				e := out.Errorf("already-ignored", "%s on %s is already ignored (%s %s)", mod, on, ig.Rule, ig.Declared)
				e.Help = "pass --force to replace it"
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
				problem, err := resolve.MatchProblem(v.Problems, mod, on, rule)
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
					return resolve.NoProblem(v.Problems, mod, on)
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
				relation := "dependency"
				if entry.Rule == "breaks" {
					relation = "break"
				}
				aside := entry.Declared
				if res.Replaced {
					aside = strings.TrimPrefix(aside+", replacing the earlier ignore", ", ")
				}
				l.OK(fmt.Sprintf("Ignoring %s's %s %s", mod, on, relation), aside)
			})
		},
	}
	a.scopeFlags(cmd)
	cmd.Flags().StringVar(&note, "note", "", "why the constraint is safe to ignore (required)")
	cmd.Flags().StringVar(&rule, "rule", "", "the problem's rule, depends or breaks (printed with the problem)")
	cmd.Flags().StringVar(&declared, "declared", "", "the range the jar declares (printed with the problem); with it, nothing is resolved")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing ignore for the pair")
	return cmd
}

func (a *app) unignoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "unignore <mod> <on>",
		Annotations: acts(),
		Short:       "Drop an ignored dependency problem so it is checked again",
		Args:        exactArgs(2),
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
				l.OK(fmt.Sprintf("Unignored %s on %s", mod, on), strings.TrimSpace(entry.Rule+" "+entry.Declared))
			})
		},
	}
	a.scopeFlags(cmd)
	return cmd
}
