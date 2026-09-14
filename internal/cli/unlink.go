package cli

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
)

type unlinkResult struct {
	config.Link
	OK       bool       `json:"ok"`
	Removed  string     `json:"removed,omitempty"`
	Relink   string     `json:"relink"`
	RelinkIn string     `json:"relinkIn,omitempty"`
	Error    *out.Error `json:"error,omitempty"`
	summary  string
}

func (a *app) unlinkCmd() *cobra.Command {
	var sel linkSelection
	cmd := &cobra.Command{
		Use:   "unlink [name | dir | launcher]",
		Short: "Stop syncing a linked instance or synced directory and forget it, keeping its files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			if query == "" && !sel.all {
				return out.Errorf("usage", "name the entry to unlink, or pass --all; `shulker links` lists them")
			}
			links, err := a.unlinkTargets(query, sel)
			if err != nil {
				return err
			}
			path, err := a.registryFile()
			if err != nil {
				return err
			}
			results := []unlinkResult{}
			failed := 0
			for _, l := range links {
				r, err := a.unlink(path, l)
				if err != nil {
					failed++
					r.OK, r.Error = false, out.AsError(err)
				}
				results = append(results, r)
			}
			if failed > 0 && len(links) == 1 {
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
						l.Nudge("To register it again", r.Relink)
					}
				}
			}
			if failed > 0 {
				if !a.printer.JSON {
					printResults(a.printer.Out())
				}
				e := out.Errorf("unlink-failed", "%d of %d entries couldn't be unlinked", failed, len(links))
				e.Data = results
				return e
			}
			return a.printer.Emit(results, printResults)
		},
	}
	sel.register(cmd, "unlink every entry the name matches, or every entry when there's no name")
	return cmd
}

func (a *app) unlink(configPath string, l config.Link) (unlinkResult, error) {
	r := unlinkResult{Link: l, OK: true}
	r.Relink, r.RelinkIn = launcher.Relink(l)
	if l.Launcher == "gdlauncher" {
		if running, _ := launcher.GDLauncherRunning(); running {
			a.printer.Warn("GDLauncher is open; it may put back the pre-launch sync this removes from %q. Quit it, then check the instance's settings", l.Name)
		}
	}
	f, err := launcher.Forget(l)
	if err != nil {
		return r, err
	}
	r.Removed, r.summary = f.Removed, f.Summary
	_, err = config.UpdateLinks(configPath, func(links []config.Link) []config.Link {
		if i, ok := config.FindLink(links, l.Dir); ok {
			return append(links[:i], links[i+1:]...)
		}
		return links
	})
	if err != nil {
		return r, err
	}
	if filepath.IsAbs(l.Source) {
		lf, err := local.Load(l.Source)
		if err == nil && lf.RemoveSyncDir(l.Target, l.Dir) {
			err = lf.Save()
		}
		if err != nil {
			a.printer.Warn("couldn't drop %s from %s in %s: %v", l.Dir, local.FileName, l.Source, err)
		}
	}
	return r, nil
}
