package cli

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type unlinkResult struct {
	config.Instance
	OK       bool       `json:"ok"`
	Removed  string     `json:"removed,omitempty"`
	Relink   string     `json:"relink"`
	RelinkIn string     `json:"relinkIn,omitempty"`
	Error    *out.Error `json:"error,omitempty"`
	summary  string
}

func (a *app) unlinkCmd() *cobra.Command {
	var sel instanceSelection
	cmd := &cobra.Command{
		Use:         "unlink [id | name | dir | launcher]",
		Annotations: acts(),
		Short:       "Stop syncing an instance and forget it, keeping its files",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			if query == "" && !sel.all {
				e := out.Errorf("usage", "name the instance to unlink, or pass --all")
				e.Help = "`shulker instances` lists them"
				return e
			}
			entries, err := a.unlinkTargets(query, sel)
			if err != nil {
				return err
			}
			path, err := a.registryFile()
			if err != nil {
				return err
			}
			results := []unlinkResult{}
			failed := 0
			for _, l := range entries {
				r, err := a.unlink(path, l)
				if err != nil {
					failed++
					r.OK, r.Error = false, out.AsError(err)
				}
				results = append(results, r)
			}
			if failed > 0 && len(entries) == 1 {
				return results[0].Error
			}
			printResults := func(l *out.Lines) {
				for i, r := range results {
					if i > 0 {
						l.Blank()
					}
					if !r.OK {
						l.Error(r.Error)
						continue
					}
					summary, rest, _ := strings.Cut(strings.TrimSuffix(r.summary, "."), "\n")
					l.OK(strings.ToLower(summary[:1])+summary[1:], "")
					if rest != "" {
						l.Tree(out.Row{Text: strings.TrimSuffix(rest, ".")})
					}
					if r.RelinkIn != "" {
						l.Nudge("To link it again, in "+r.RelinkIn, r.Relink)
					} else if r.Launcher != "" {
						l.Nudge("To link it again", r.Relink)
					} else {
						l.Nudge("To sync it again", r.Relink)
					}
				}
			}
			if failed > 0 {
				if !a.printer.JSON {
					printResults(a.printer.Out())
				}
				e := out.Errorf("unlink-failed", "%d of %d instances couldn't be unlinked", failed, len(entries))
				e.Data = results
				return e
			}
			return a.printer.Emit(results, printResults)
		},
	}
	sel.register(cmd, "unlink every entry the name matches, or every entry when there's no name")
	return cmd
}

func (a *app) unlink(configPath string, l project.InstanceEntry) (unlinkResult, error) {
	r := unlinkResult{Instance: l.Instance, OK: true}
	r.Relink, r.RelinkIn = launcher.Relink(launcher.Linked{Instance: l.Instance, Side: l.Side, AssumesClient: l.AssumesClient, Ref: l.Ref, Path: l.Path})
	f, err := launcher.Forget(l.Instance)
	if err != nil {
		return r, err
	}
	if f.Warning != "" {
		a.printer.Warn("%s", f.Warning)
	}
	r.Removed, r.summary = f.Removed, f.Summary
	if inf, err := instance.Load(l.Dir); err == nil {
		inf.IsUnlinked = true
		if err := inf.Save(l.Dir); err != nil {
			return r, err
		}
	}
	_, err = config.UpdateInstances(configPath, func(instances []config.Instance) []config.Instance {
		if i, ok := config.FindInstance(instances, l.Dir); ok {
			return append(instances[:i], instances[i+1:]...)
		}
		return instances
	})
	if err != nil {
		return r, err
	}
	if filepath.IsAbs(l.Source) {
		lf, err := a.loadLocal(l.Source)
		if err == nil && lf.RemoveSyncDir(l.Side, l.Dir) {
			err = lf.Save()
		}
		if err != nil {
			a.printer.Warn("couldn't drop %s from %s in %s: %v", l.Dir, local.FileName, l.Source, err)
		}
	}
	return r, nil
}
