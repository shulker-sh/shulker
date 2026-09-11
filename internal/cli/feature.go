package cli

import (
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type featureFlags struct {
	with, without []string
}

func (f *featureFlags) register(cmd *cobra.Command, scope string) {
	cmd.Flags().StringArrayVar(&f.with, "with", nil, "turn a feature on "+scope+"; repeat for more")
	cmd.Flags().StringArrayVar(&f.without, "without", nil, "turn a feature off "+scope+"; repeat for more")
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

func mergeDecisions(layers ...map[string]bool) map[string]bool {
	merged := map[string]bool{}
	for _, l := range layers {
		maps.Copy(merged, l)
	}
	return merged
}

func (a *app) refreshLocal(lf *local.File, inProject, changed bool) {
	if !changed && (!lf.Exists() || lf.DetectedOS == build.DetectOS()) {
		return
	}
	if err := a.saveLocal(lf, inProject); err != nil {
		a.progress("warning: %s not updated: %v", local.FileName, err)
	}
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

type featureScope struct {
	project   *project.Project
	file      *local.File
	decisions map[string]bool
	into      string
	source    *syncSource
	state     build.State
}

func (a *app) openFeatures(cmd *cobra.Command, into string) (*featureScope, *build.Builder, error) {
	var sc *featureScope
	var err error
	if into == "" {
		sc, err = a.projectFeatures()
	} else {
		sc, err = a.instanceFeatures(cmd, into, true)
	}
	if err != nil {
		return nil, nil, err
	}
	b, err := a.builder(cmd.Context(), sc.project)
	if err != nil {
		return nil, nil, err
	}
	return sc, b, nil
}

func (a *app) projectFeatures() (*featureScope, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, err
	}
	lf, err := local.Load(p.Dir)
	if err != nil {
		return nil, err
	}
	return &featureScope{project: p, file: lf, decisions: lf.Features}, nil
}

func (a *app) instanceFeatures(cmd *cobra.Command, into string, withSource bool) (*featureScope, error) {
	dir, err := filepath.Abs(into)
	if err != nil {
		return nil, err
	}
	sc := &featureScope{into: dir, state: build.LoadState(dir)}
	if sc.file, err = local.Load(dir); err != nil {
		return nil, err
	}
	sc.decisions = sc.file.Features
	if !withSource {
		return sc, nil
	}
	if sc.state.Source == "" {
		return nil, out.Errorf("not-synced", "%s has no record of the source it was synced from; run `shulker sync <source> --into %s` once", dir, dir)
	}
	if sc.source, err = a.openSource(cmd.Context(), sc.state.Source, sc.state.Ref); err != nil {
		return nil, err
	}
	sc.project = sc.source.project
	proj, _, err := sourceLocalFiles(sc.source, dir)
	if err != nil {
		return nil, err
	}
	sc.decisions = mergeDecisions(proj.Features, sc.file.Features)
	return sc, nil
}

func (a *app) resync(cmd *cobra.Command, sc *featureScope) (*syncResult, error) {
	if sc.source == nil {
		var err error
		if sc, err = a.instanceFeatures(cmd, sc.into, true); err != nil {
			return nil, err
		}
	}
	res, err := a.sync(cmd.Context(), sc.source, syncRequest{ref: sc.state.Ref, target: sc.state.Target, into: sc.into})
	return &res, err
}

func registerFeatureChange(cmd *cobra.Command) {
	registerFeatureScope(cmd, "change the choice for")
	cmd.Flags().Bool("sync", false, "sync the --into or --instance directory from its recorded source right away")
}

func registerFeatureScope(cmd *cobra.Command, verb string) {
	cmd.Flags().String("into", "", verb+" a synced directory instead of this project")
	cmd.Flags().String("instance", "", verb+" a linked instance or synced directory, by name or directory")
	var sel linkSelection
	sel.register(cmd, "")
}

// featureInto resolves --instance (narrowed by --launcher and --side) to the entry's directory.
func (a *app) featureInto(cmd *cobra.Command) (string, error) {
	into, _ := cmd.Flags().GetString("into")
	instance, _ := cmd.Flags().GetString("instance")
	var sel linkSelection
	sel.launcher, _ = cmd.Flags().GetString("launcher")
	sel.side, _ = cmd.Flags().GetString("side")
	if instance == "" {
		if sel.narrows() {
			return "", out.Errorf("usage", "--launcher and --side narrow --instance")
		}
		return into, nil
	}
	if into != "" {
		return "", out.Errorf("usage", "pass --into or --instance, not both")
	}
	links, err := a.selectLinks(instance, sel)
	if err != nil {
		return "", err
	}
	return links[0].Dir, nil
}

func (a *app) featureChangeFlags(cmd *cobra.Command) (into string, sync bool, err error) {
	if into, err = a.featureInto(cmd); err != nil {
		return "", false, err
	}
	sync, _ = cmd.Flags().GetBool("sync")
	if sync && into == "" {
		return "", false, out.Errorf("usage", "--sync needs --into or --instance; in a project, run `shulker build`")
	}
	return into, sync, nil
}

func (a *app) emitFeatureChange(cmd *cobra.Command, sc *featureScope, sync, changed bool, res map[string]any, line string) error {
	var synced *syncResult
	if sync {
		var err error
		if synced, err = a.resync(cmd, sc); err != nil {
			return err
		}
		res["sync"] = synced
	}
	if sc.into != "" {
		res["into"] = sc.into
	}
	return a.printer.Emit(res, func(w io.Writer) {
		switch {
		case sc.into == "":
			fmt.Fprintln(w, line)
		case changed && synced == nil:
			fmt.Fprintf(w, "%s in %s; takes effect on the next sync (a linked Prism instance syncs on launch)\n", line, sc.into)
		default:
			fmt.Fprintf(w, "%s in %s\n", line, sc.into)
		}
		if synced != nil {
			synced.print(w)
		}
	})
}

func (a *app) featureSetCmd(verb string, on bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   verb + " <feature>",
		Short: fmt.Sprintf("Turn a feature %s for every target on this machine", verb),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			into, sync, err := a.featureChangeFlags(cmd)
			if err != nil {
				return err
			}
			sc, b, err := a.openFeatures(cmd, into)
			if err != nil {
				return err
			}
			if known := featureNames(b.Features()); !slices.Contains(known, name) {
				return unknownFeature(name, known)
			}
			sc.file.SetFeature(name, on)
			if err := a.saveLocal(sc.file, into == ""); err != nil {
				return err
			}
			line := name + " " + verb
			if into == "" {
				line += "; takes effect on the next build or sync"
			}
			return a.emitFeatureChange(cmd, sc, sync, true, map[string]any{"feature": name, "on": on}, line)
		},
	}
	registerFeatureChange(cmd)
	return cmd
}

func (a *app) featureResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset <feature>",
		Short: "Forget your choice for a feature and follow the target defaults again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			into, sync, err := a.featureChangeFlags(cmd)
			if err != nil {
				return err
			}
			var sc *featureScope
			if into == "" {
				sc, err = a.projectFeatures()
			} else {
				sc, err = a.instanceFeatures(cmd, into, false)
			}
			if err != nil {
				return err
			}
			had := sc.file.ResetFeature(name)
			if had {
				if err := a.saveLocal(sc.file, into == ""); err != nil {
					return err
				}
			}
			line := name + " follows the target defaults again"
			if !had {
				line = name + " had no choice to reset"
			}
			return a.emitFeatureChange(cmd, sc, sync, had, map[string]any{"feature": name, "reset": had}, line)
		},
	}
	registerFeatureChange(cmd)
	return cmd
}

func (a *app) featureListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List features with the mods they gate, target defaults, and your choices",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			into, err := a.featureInto(cmd)
			if err != nil {
				return err
			}
			sc, b, err := a.openFeatures(cmd, into)
			if err != nil {
				return err
			}
			targets := targetNames(sc.project.Manifest.Targets)
			res := []featureStatus{}
			for _, f := range b.Features() {
				st := featureStatus{Feature: f, Effective: map[string]bool{}}
				if on, ok := sc.decisions[f.Name]; ok {
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
	registerFeatureScope(cmd, "list the choices for")
	return cmd
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
