package cli

import (
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

var targetNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type targetEntry struct {
	ID string `json:"target"`
	manifest.Target
}

func (a *app) targetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Manage the targets shulker.json builds",
	}
	cmd.AddCommand(a.targetAddCmd(), a.targetRemoveCmd(), a.targetListCmd())
	return cmd
}

func (a *app) targetAddCmd() *cobra.Command {
	var (
		t    manifest.Target
		vars []string
	)
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a build target to shulker.json",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !targetNamePattern.MatchString(name) {
				return out.Errorf("usage", "target name %q must start with a lowercase letter and use only a-z, 0-9, _ and -", name)
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if _, ok := p.Manifest.Targets[name]; ok {
				return out.Errorf("target-exists", "target %s is already in the manifest", name)
			}
			if t.Side == "" && (name == "client" || name == "server") {
				t.Side = name
			}
			if t.Side != "client" && t.Side != "server" {
				return out.Errorf("usage", "pass --side client or --side server for target %s", name)
			}
			if t.Build == "" {
				t.Build = "build/" + name
			}
			for _, kv := range vars {
				key, value, ok := strings.Cut(kv, "=")
				if !ok || key == "" {
					return out.Errorf("usage", "--var takes key=value, got %q", kv)
				}
				if t.Variables == nil {
					t.Variables = manifest.Variables{}
				}
				t.Variables[key] = value
			}
			if p.Manifest.Targets == nil {
				p.Manifest.Targets = map[string]manifest.Target{}
			}
			p.Manifest.Targets[name] = t
			if err := p.SaveManifest(); err != nil {
				return err
			}
			return a.printer.Emit(targetEntry{ID: name, Target: t}, func(l *out.Lines) {
				l.Items(out.Item{Kind: out.Add, Name: name, Text: l.T.Grey(l.T.ArrowInto()) + " " + l.T.Grey(t.Build), Aside: []string{t.Side + " target"}})
				l.Nudge("Build it", "shulker build "+name)
			})
		},
	}
	cmd.Flags().StringVar(&t.Side, "side", "", "client or server (default: the target name when it is client or server)")
	cmd.Flags().StringVar(&t.Build, "build", "", "output directory (default: build/<name>)")
	cmd.Flags().StringArrayVar(&t.Overrides, "overrides", []string{"overrides"}, "override layer, applied in order; repeat for more")
	cmd.Flags().StringArrayVar(&t.Features, "feature", nil, "feature on by default for this target; repeat for more")
	cmd.Flags().StringArrayVar(&t.WholeFiles, "whole-file", nil, ".properties override path or glob to copy whole instead of merging per key; repeat for more")
	cmd.Flags().StringVar(&t.Name, "name", "", "display name launchers show (default: the manifest name)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "template variable as key=value; repeat for more")
	cmd.Flags().StringVar(&t.Note, "note", "", "free-form note kept in shulker.json")
	return cmd
}

func (a *app) targetRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a target from shulker.json, leaving its build directory in place",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			p, err := a.openProject()
			if err != nil {
				return err
			}
			t, err := p.Manifest.Target(name)
			if err != nil {
				return err
			}
			if len(p.Manifest.Targets) == 1 {
				return out.Errorf("last-target", "%s is the only target; add another with `shulker target add` before removing it", name)
			}
			t.Build = p.Manifest.BuildDir(name)
			delete(p.Manifest.Targets, name)
			if err := p.SaveManifest(); err != nil {
				return err
			}
			return a.printer.Emit(targetEntry{ID: name, Target: t}, func(l *out.Lines) {
				l.Items(out.Item{Kind: out.Drop, Name: name, Aside: []string{"left " + t.Build + " on disk"}})
			})
		},
	}
}

func (a *app) targetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List targets with their side, build directory, overrides, and features",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			res := []targetEntry{}
			for _, name := range targetNames(p.Manifest.Targets) {
				t := p.Manifest.Targets[name]
				t.Build = p.Manifest.BuildDir(name)
				res = append(res, targetEntry{ID: name, Target: t})
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				var items []out.Item
				for _, e := range res {
					aside := []string{e.Side + " target", "overrides: " + strings.Join(e.Overrides, ", ")}
					if len(e.Features) > 0 {
						aside = append(aside, "features: "+strings.Join(e.Features, ", "))
					}
					if e.Name != "" {
						aside = append(aside, "shown as "+e.Name)
					}
					items = append(items, out.Item{Kind: out.Note, Name: e.ID, Text: l.T.Grey(l.T.ArrowInto()) + " " + l.T.Grey(e.Build), Aside: aside})
				}
				l.Items(items...)
			})
		},
	}
}
