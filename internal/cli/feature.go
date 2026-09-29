package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

type featureFlags struct {
	with, without []string
}

func (f *featureFlags) register(cmd *cobra.Command, scope string) {
	cmd.Flags().StringArrayVar(&f.with, "with", nil, "turn a feature on "+scope+"; repeat for more")
	cmd.Flags().StringArrayVar(&f.without, "without", nil, "turn a feature off "+scope+"; repeat for more")
}

func (f featureFlags) check(b *build.Builder) error {
	_, err := sync.FeatureOverrides(b, f.with, f.without, nil)
	return err
}

// overrides is the feature decisions a build takes, once the flags name features the build knows.
func (f featureFlags) overrides(b *build.Builder, decisions map[string]bool) (map[string]bool, error) {
	return sync.FeatureOverrides(b, f.with, f.without, decisions)
}

func checkOS(name string) error {
	if name != "" && !build.ValidOS(name) {
		return out.Errorf("usage", "--os must be macos, windows, or linux, not %q", name)
	}
	return nil
}

func (a *app) saveLocal(lf *local.File, inProject bool) error {
	se, err := a.syncEnv()
	if err != nil {
		return err
	}
	return sync.SaveLocal(se, lf, inProject)
}

func (a *app) refreshLocal(lf *local.File, inProject, changed bool) {
	if se, err := a.syncEnv(); err == nil {
		sync.RefreshLocal(se, lf, inProject, changed)
	}
}

type featureStatus struct {
	build.Feature
	Choice    *bool `json:"choice"`
	Effective bool  `json:"effective"`
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
	source    *sync.Source
	state     instance.State
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
	lf, err := a.loadLocal(p.Dir)
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
	state, stateErr := instance.ReadState(dir)
	a.warnState(stateErr, a.forceCommand(nil, "", dir))
	sc := &featureScope{into: dir, state: state}
	if sc.file, err = a.loadLocal(dir); err != nil {
		return nil, err
	}
	sc.decisions = sc.file.Features
	if !withSource {
		return sc, nil
	}
	if sc.state.Source == "" {
		e := out.Errorf("not-synced", "%s has no record of the source it was synced from", dir)
		e.Help = fmt.Sprintf("run `shulker sync <source> --into %s` once", dir)
		return nil, e
	}
	if sc.source, err = a.openSource(cmd.Context(), sc.state.Source, modpack.At{Ref: sc.state.Ref, Path: sc.state.Path}); err != nil {
		return nil, err
	}
	sc.project = sc.source.Project
	se, err := a.syncEnv()
	if err != nil {
		return nil, err
	}
	proj, _, err := sync.SourceLocalFiles(se, sc.source, dir)
	if err != nil {
		return nil, err
	}
	sc.decisions = build.MergeDecisions(proj.Features, sc.file.Features)
	return sc, nil
}

func (a *app) resync(cmd *cobra.Command, sc *featureScope) (*syncResult, error) {
	if sc.source == nil {
		var err error
		if sc, err = a.instanceFeatures(cmd, sc.into, true); err != nil {
			return nil, err
		}
	}
	intent, err := instance.Load(sc.into)
	assume := err == nil && intent.AssumesClient
	res, err := a.sync(cmd.Context(), sc.source, syncRequest{Request: sync.Request{Side: sc.state.Side, Into: sc.into, AssumeClient: assume}})
	return &res, err
}

// featureWhere is the scope flags every feature command takes: the project
// itself, a synced directory (--into), or a linked instance (--instance,
// narrowed by --launcher and --side).
type featureWhere struct {
	into string
	sync bool
	sel  instanceSelection
}

func (f *featureWhere) register(cmd *cobra.Command, verb string) {
	cmd.Flags().StringVar(&f.into, "into", "", verb+" a synced directory instead of this project")
	f.sel.register(cmd, "")
}

func (f *featureWhere) registerChange(cmd *cobra.Command) {
	f.register(cmd, "change the choice for")
	cmd.Flags().BoolVar(&f.sync, "sync", false, "sync the --into or -i directory from its recorded source right away")
}

// featureDir resolves -i (narrowed by --launcher and --side) to the instance's directory.
func (a *app) featureDir(f *featureWhere) (string, error) {
	if a.instance == "" {
		if f.sel.narrows() {
			return "", out.Errorf("usage", "--launcher and --side narrow -i")
		}
		return f.into, nil
	}
	if f.into != "" {
		return "", out.Errorf("usage", "pass --into or -i, not both")
	}
	entries, err := a.selectInstances(a.instance, f.sel)
	if err != nil {
		return "", err
	}
	return entries[0].Dir, nil
}

func (a *app) featureChangeDir(f *featureWhere) (string, error) {
	into, err := a.featureDir(f)
	if err != nil {
		return "", err
	}
	if f.sync && into == "" {
		e := out.Errorf("usage", "--sync needs --into or -i")
		e.Help = "in a project, run `shulker build`"
		return "", e
	}
	return into, nil
}

type featureChange struct {
	Feature string      `json:"feature"`
	On      *bool       `json:"on,omitempty"`
	Reset   *bool       `json:"reset,omitempty"`
	Into    string      `json:"into,omitempty"`
	Sync    *syncResult `json:"sync,omitempty"`
}

func (a *app) emitFeatureChange(cmd *cobra.Command, sc *featureScope, sync, changed bool, res featureChange, line string) error {
	var synced *syncResult
	if sync {
		var err error
		if synced, err = a.resync(cmd, sc); err != nil {
			return err
		}
		res.Sync = synced
	}
	res.Into = sc.into
	return a.printer.Emit(res, func(l *out.Lines) {
		switch {
		case sc.into == "":
			l.OK(line, "")
		case changed && synced == nil:
			l.OKInto(line, sc.into, "takes effect on the next sync; a linked Prism instance syncs on launch")
		default:
			l.OKInto(line, sc.into, "")
		}
		if synced != nil {
			synced.print(l)
		}
	})
}

func (a *app) featureSetCmd(verb string, on bool) *cobra.Command {
	var where featureWhere
	cmd := &cobra.Command{
		Use:         verb + " <feature>",
		Annotations: acts(),
		Short:       fmt.Sprintf("Turn a feature %s for every side on this machine", verb),
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			into, err := a.featureChangeDir(&where)
			if err != nil {
				return err
			}
			sc, b, err := a.openFeatures(cmd, into)
			if err != nil {
				return err
			}
			if known := sync.FeatureNames(b.Features()); !slices.Contains(known, name) {
				return sync.UnknownFeature(name, known)
			}
			sc.file.SetFeature(name, on)
			if err := a.saveLocal(sc.file, into == ""); err != nil {
				return err
			}
			line := name + " " + verb
			if into == "" {
				line += " (takes effect on the next build or sync)"
			}
			return a.emitFeatureChange(cmd, sc, where.sync, true, featureChange{Feature: name, On: &on}, line)
		},
	}
	a.scopeFlags(cmd)
	where.registerChange(cmd)
	return cmd
}

func (a *app) featureResetCmd() *cobra.Command {
	var where featureWhere
	cmd := &cobra.Command{
		Use:         "reset <feature>",
		Annotations: acts(),
		Short:       "Forget your choice for a feature and follow its default again",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			into, err := a.featureChangeDir(&where)
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
			line := name + " follows its default again"
			if !had {
				line = name + " had no choice to reset"
			}
			return a.emitFeatureChange(cmd, sc, where.sync, had, featureChange{Feature: name, Reset: &had}, line)
		},
	}
	a.scopeFlags(cmd)
	where.registerChange(cmd)
	return cmd
}

func (a *app) featureListCmd() *cobra.Command {
	var where featureWhere
	cmd := &cobra.Command{
		Use:         "list",
		Annotations: reads(),
		Aliases:     []string{"ls"},
		Short:       "List features with the mods they gate, their defaults, and your choices",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			into, err := a.featureDir(&where)
			if err != nil {
				return err
			}
			sc, b, err := a.openFeatures(cmd, into)
			if err != nil {
				return err
			}
			res := []featureStatus{}
			for _, f := range b.Features() {
				st := featureStatus{Feature: f, Effective: f.Default}
				if on, ok := sc.decisions[f.Name]; ok {
					st.Choice, st.Effective = &on, on
				}
				res = append(res, st)
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				if len(res) == 0 {
					l.Info("No features")
					return
				}
				var items []out.Item
				for _, st := range res {
					state, reason, _ := strings.Cut(st.state(), " (")
					var aside []string
					if len(st.Mods) > 0 {
						aside = []string{"gates: " + strings.Join(st.Mods, ", ")}
					}
					if st.Origin != "" {
						aside = append([]string{"from " + st.Origin}, aside...)
					}
					if reason != "" {
						aside = append([]string{strings.TrimSuffix(reason, ")")}, aside...)
					}
					items = append(items, out.Item{Kind: out.Note, Name: st.Name, Text: state, Aside: aside})
				}
				l.Items(items...)
			})
		},
	}
	a.scopeFlags(cmd)
	where.register(cmd, "list the choices for")
	return cmd
}

func (f featureStatus) state() string {
	if f.Choice != nil {
		if *f.Choice {
			return "on (your choice)"
		}
		return "off (your choice)"
	}
	if f.Default {
		return "on (default)"
	}
	return "off"
}
