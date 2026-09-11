package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/shulker-sh/shulker/internal/manifest"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/spf13/cobra"
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
		Args:  cobra.ExactArgs(1),
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
					t.Variables = map[string]string{}
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
			return a.printer.Emit(targetEntry{ID: name, Target: t}, func(w io.Writer) {
				fmt.Fprintf(w, "+ target %s (%s, %s)\n", name, t.Side, t.Build)
				fmt.Fprintf(w, "Next: shulker build %s\n", name)
			})
		},
	}
	cmd.Flags().StringVar(&t.Side, "side", "", "client or server (default: the target name when it is client or server)")
	cmd.Flags().StringVar(&t.Build, "build", "", "output directory (default: build/<name>)")
	cmd.Flags().StringArrayVar(&t.Overrides, "overrides", []string{"overrides"}, "override layer, applied in order; repeat for more")
	cmd.Flags().StringArrayVar(&t.Features, "feature", nil, "feature on by default for this target; repeat for more")
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
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			p, err := a.openProject()
			if err != nil {
				return err
			}
			t, ok := p.Manifest.Targets[name]
			if !ok {
				e := out.Errorf("target-not-found", "no target %q in shulker.json", name)
				e.Candidates = targetNames(p.Manifest.Targets)
				return e
			}
			if len(p.Manifest.Targets) == 1 {
				return out.Errorf("last-target", "%s is the only target; add another with `shulker target add` before removing it", name)
			}
			delete(p.Manifest.Targets, name)
			if err := p.SaveManifest(); err != nil {
				return err
			}
			return a.printer.Emit(targetEntry{ID: name, Target: t}, func(w io.Writer) {
				fmt.Fprintf(w, "- target %s\n", name)
				fmt.Fprintf(w, "  left %s on disk\n", t.Build)
			})
		},
	}
}

func (a *app) targetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List targets with their side, build directory, overrides, and features",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			res := []targetEntry{}
			for _, name := range targetNames(p.Manifest.Targets) {
				res = append(res, targetEntry{ID: name, Target: p.Manifest.Targets[name]})
			}
			return a.printer.Emit(res, func(w io.Writer) {
				for _, e := range res {
					fmt.Fprintf(w, "%s %s %s overrides=%s", e.ID, e.Side, e.Build, strings.Join(e.Overrides, ","))
					if len(e.Features) > 0 {
						fmt.Fprintf(w, " features=%s", strings.Join(e.Features, ","))
					}
					if e.Name != "" {
						fmt.Fprintf(w, " %q", e.Name)
					}
					fmt.Fprintln(w)
				}
			})
		},
	}
}
