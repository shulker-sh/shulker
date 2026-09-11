package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/shulker-sh/shulker/internal/build"
	"github.com/shulker-sh/shulker/internal/local"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/shulker-sh/shulker/internal/project"
	"github.com/spf13/cobra"
)

type featureFlags struct {
	with, without []string
}

func (f *featureFlags) register(cmd *cobra.Command, scope string) {
	cmd.Flags().StringArrayVar(&f.with, "with", nil, "turn a feature on "+scope+"; repeat for more")
	cmd.Flags().StringArrayVar(&f.without, "without", nil, "turn a feature off "+scope+"; repeat for more")
}

func (f featureFlags) args() []string {
	var args []string
	for _, name := range f.with {
		args = append(args, "--with", name)
	}
	for _, name := range f.without {
		args = append(args, "--without", name)
	}
	return args
}

func (f featureFlags) check(b *build.Builder) error {
	for _, name := range f.with {
		if slices.Contains(f.without, name) {
			return out.Errorf("usage", "--with and --without both name %s", name)
		}
	}
	known := featureNames(b.Features())
	for _, name := range append(slices.Clone(f.with), f.without...) {
		if !slices.Contains(known, name) {
			return unknownFeature(name, known)
		}
	}
	return nil
}

func featureOverrides(b *build.Builder, decisions map[string]bool, f featureFlags) (map[string]bool, error) {
	if err := f.check(b); err != nil {
		return nil, err
	}
	overrides := map[string]bool{}
	for name, on := range decisions {
		overrides[name] = on
	}
	for _, name := range f.with {
		overrides[name] = true
	}
	for _, name := range f.without {
		overrides[name] = false
	}
	return overrides, nil
}

func featureNames(features []build.Feature) []string {
	names := make([]string, 0, len(features))
	for _, f := range features {
		names = append(names, f.Name)
	}
	return names
}

func unknownFeature(name string, known []string) error {
	e := out.Errorf("feature-not-found", "no mod or target in shulker.json uses feature %q", name)
	e.Candidates = known
	return e
}

func checkOS(name string) error {
	if name != "" && !build.ValidOS(name) {
		return out.Errorf("usage", "--os must be macos, windows, or linux, not %q", name)
	}
	return nil
}

func (a *app) saveLocal(lf *local.File, inProject bool) error {
	created := !lf.Exists()
	lf.DetectedOS = build.DetectOS()
	if err := lf.Save(); err != nil {
		return err
	}
	if !created || !inProject {
		return nil
	}
	added, err := local.AddToGitignore(lf.Dir())
	if added {
		a.progress("added /%s to .gitignore", local.FileName)
	}
	return err
}

func (a *app) refreshLocal(lf *local.File, inProject bool) error {
	if !lf.Exists() {
		return nil
	}
	return a.saveLocal(lf, inProject)
}

type featureStatus struct {
	build.Feature
	Choice    *bool           `json:"choice"`
	Effective map[string]bool `json:"effective"`
}

func (a *app) featureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feature",
		Short: "Turn optional mods on or off for this machine",
	}
	cmd.AddCommand(a.featureSetCmd("on", true), a.featureSetCmd("off", false), a.featureResetCmd(), a.featureListCmd())
	return cmd
}

func (a *app) openFeatures(cmd *cobra.Command) (*project.Project, *build.Builder, *local.File, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, nil, nil, err
	}
	b, err := a.builder(cmd.Context(), p)
	if err != nil {
		return nil, nil, nil, err
	}
	lf, err := local.Load(p.Dir)
	if err != nil {
		return nil, nil, nil, err
	}
	return p, b, lf, nil
}

func (a *app) featureSetCmd(verb string, on bool) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <feature>",
		Short: fmt.Sprintf("Turn a feature %s for every target on this machine", verb),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			_, b, lf, err := a.openFeatures(cmd)
			if err != nil {
				return err
			}
			if known := featureNames(b.Features()); !slices.Contains(known, name) {
				return unknownFeature(name, known)
			}
			lf.SetFeature(name, on)
			if err := a.saveLocal(lf, true); err != nil {
				return err
			}
			return a.printer.Emit(map[string]any{"feature": name, "on": on}, func(w io.Writer) {
				fmt.Fprintf(w, "%s %s; takes effect on the next build or sync\n", name, verb)
			})
		},
	}
}

func (a *app) featureResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <feature>",
		Short: "Forget your choice for a feature and follow the target defaults again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			p, err := a.openProject()
			if err != nil {
				return err
			}
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			had := lf.ResetFeature(name)
			if had {
				if err := a.saveLocal(lf, true); err != nil {
					return err
				}
			}
			return a.printer.Emit(map[string]any{"feature": name, "reset": had}, func(w io.Writer) {
				if had {
					fmt.Fprintf(w, "%s follows the target defaults again\n", name)
				} else {
					fmt.Fprintf(w, "%s had no choice to reset\n", name)
				}
			})
		},
	}
}

func (a *app) featureListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List features with the mods they gate, target defaults, and your choices",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, b, lf, err := a.openFeatures(cmd)
			if err != nil {
				return err
			}
			targets := targetNames(p.Manifest.Targets)
			res := []featureStatus{}
			for _, f := range b.Features() {
				st := featureStatus{Feature: f, Effective: map[string]bool{}}
				if on, ok := lf.Features[f.Name]; ok {
					st.Choice = &on
				}
				for _, t := range targets {
					st.Effective[t] = slices.Contains(f.Defaults, t)
					if st.Choice != nil {
						st.Effective[t] = *st.Choice
					}
				}
				res = append(res, st)
			}
			return a.printer.Emit(res, func(w io.Writer) {
				if len(res) == 0 {
					fmt.Fprintln(w, "No features.")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				for _, st := range res {
					fmt.Fprintf(tw, "%s\t%s\tgates: %s\n", st.Name, st.state(len(targets)), strings.Join(st.Mods, ", "))
				}
				tw.Flush()
			})
		},
	}
}

func (st featureStatus) state(targets int) string {
	if st.Choice != nil {
		if *st.Choice {
			return "on (your choice)"
		}
		return "off (your choice)"
	}
	switch len(st.Defaults) {
	case 0:
		return "off"
	case targets:
		return "on (target default)"
	}
	return "on in " + strings.Join(st.Defaults, ", ") + " (target default)"
}
