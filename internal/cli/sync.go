package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

type syncResult struct {
	sync.Result
	Changes   *lockChanges         `json:"changes,omitempty"`
	Instances []syncInstanceResult `json:"instances,omitempty"`
}

// syncRequest is a sync's flags: what the sync module takes, and the take-over command line
// only the CLI's state nudge needs.
type syncRequest struct {
	sync.Request
	features featureFlags
	// rerun is the take-over command for a state file sync can't read; empty names the command that
	// rebuilds the directory.
	rerun string
}

func (r syncRequest) request() sync.Request {
	q := r.Request
	q.With, q.Without = r.features.with, r.features.without
	return q
}

func (a *app) syncCmd() *cobra.Command {
	var req syncRequest
	var sel instanceSelection
	var offline bool
	cmd := &cobra.Command{
		Use:         "sync [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Short:       "Download and build one side of a project straight into a directory",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(req.OS); err != nil {
				return err
			}
			req.Backup = "sync"
			if !req.Force {
				req.rerun = rerunForced(cmd, args)
			}
			if offline {
				d, err := a.deps()
				if err != nil {
					return err
				}
				d.Fetch.Offline = true
			}
			if len(args) == 0 {
				if req.Into != "" && req.Side == "" && req.At == (modpack.At{}) && a.instance == "" && !sel.all && !sel.narrows() {
					res, err := a.syncRecorded(cmd, req)
					if err != nil {
						return err
					}
					return a.printer.Emit(res, res.print)
				}
				if req.Into != "" || req.At != (modpack.At{}) {
					return out.Errorf("usage", "--into, --ref and --path need a source; a registered instance already has them")
				}
				if a.instance == "" && !sel.all {
					dir, err := a.scopeDir()
					if err != nil {
						return err
					}
					if p, side, ok, err := sync.InPlaceProject(dir); err != nil {
						return err
					} else if ok {
						return a.syncTree(cmd, p, side, sel, req)
					}
					entries, inProject, err := a.projectInstances(sel)
					if err != nil {
						return err
					}
					if inProject {
						return a.syncInstances(cmd, entries, req)
					}
				}
				entries, err := a.selectInstances(a.instance, sel)
				if err != nil && a.instance != "" {
					entries, err = a.selectProjectDetached(a.instance, sel, err)
				}
				if err != nil {
					return err
				}
				if a.instance == "" && !sel.all {
					picked, err := a.pickInstance(entries)
					if err != nil {
						return err
					}
					entries = []project.InstanceEntry{picked}
				}
				if sel.all {
					return a.syncInstances(cmd, entries, req)
				}
				res, err := a.syncInstance(cmd, entries[0], req)
				if err != nil {
					return err
				}
				return a.printer.Emit(res, res.print)
			}
			if a.instance != "" || sel.all {
				return out.Errorf("usage", "pass a source or -i/--all, not both")
			}
			if sel.Launcher != "" {
				return out.Errorf("usage", "--launcher narrows -i, --all, or the picker; it doesn't apply to a source")
			}
			req.Side = sel.Side
			src, err := a.openSource(cmd.Context(), args[0], req.At)
			if err != nil {
				return err
			}
			res, err := a.sync(cmd.Context(), src, req)
			if err != nil {
				return err
			}
			return a.printer.Emit(res, res.print)
		},
	}
	cmd.Flags().StringVar(&req.Into, "into", "", "output directory (default: the side's build directory)")
	cmd.Flags().BoolVar(&req.Force, "force", false, "overwrite files edited in the output directory, seeded files included")
	cmd.Flags().BoolVar(&req.AssumeClient, "assume-client", false, "build a client even when the source declares none, from the mods and overrides both sides share")
	cmd.Flags().StringVar(&req.At.Ref, "ref", "", "branch, tag, or commit to sync from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&req.At.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	cmd.Flags().StringVar(&req.OS, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	sel.registerWith(cmd, "sync every instance (narrow with --launcher or --side)", "side to build from a source (default: the only declared side); with -i, --all, or the picker, only client or server instances")
	a.registerFailFast(cmd)
	cmd.Flags().BoolVar(&offline, "offline", false, "don't use the network; build from the last successful sync and cached files")
	req.features.register(cmd, "for this run only")
	return cmd
}

func (s syncResult) print(l *out.Lines) {
	if s.Changes != nil {
		s.Changes.printItems(l)
	}
	l.OKInto("synced "+s.Side, s.Dir, reportAside(s.Build))
	rows := reportDetailRows(l, s.Build)
	if row, ok := savesRow(s.Saves); ok {
		rows = append(rows, row)
	}
	l.Tree(rows...)
}

// syncEnv is the sync module's env for this run, built once so its backups happen once.
func (a *app) syncEnv() (*sync.Env, error) {
	if a.se != nil {
		return a.se, nil
	}
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		cfg = config.Config{}
	}
	r, err := a.rootsOf(path, cfg)
	if err != nil {
		return nil, err
	}
	keep, err := a.saveBackups()
	if err != nil {
		a.printer.Warn("couldn't read play.saveBackups, keeping %d automatic backups: %v", keep, err)
	}
	a.se = &sync.Env{Env: d.Env, Registry: config.RegistryPath(path, cfg), Saves: r.saves(), SaveBackups: keep}
	if a.canPick() {
		a.se.AskUnlock = func(key, minecraft string) (bool, error) {
			return a.askYes(fmt.Sprintf("Unlock %s and resolve its mods for Minecraft %s?", key, minecraft))
		}
	}
	return a.se, nil
}

func (a *app) openSource(ctx context.Context, from string, at modpack.At) (*sync.Source, error) {
	se, err := a.syncEnv()
	if err != nil {
		return nil, err
	}
	src, err := sync.Open(ctx, se, from, at)
	if err != nil {
		return nil, err
	}
	a.printer.LockStale = src.Project.IsLockStale()
	return src, nil
}

func (a *app) sync(ctx context.Context, src *sync.Source, req syncRequest) (syncResult, error) {
	se, err := a.syncEnv()
	if err != nil {
		return syncResult{}, err
	}
	res, err := sync.Run(ctx, se, src, req.request())
	return a.synced(res, req, err)
}

// synced is a sync's result as the command prints it: the state and kept-conflict warnings with
// their take-over commands beneath, and the relock's changes when one was saved.
func (a *app) synced(res sync.Result, req syncRequest, err error) (syncResult, error) {
	if err != nil {
		return syncResult{}, a.lastOf(err)
	}
	a.printer.LockStale = res.Project.IsLockStale()
	a.warnKeptConflicts(res.Build.KeptConflicts, res.Project, res.Side, res.Dir)
	a.warnState(res.Build.State, a.syncTakeOver(req, res.Project, res.Side, res.Dir))
	r := syncResult{Result: res}
	if res.Relock != nil && res.Relock.WasSaved {
		r.Changes = a.lockChangesOf(res.Project, res.Relock)
		a.printer.LockStale = false
	}
	return r, nil
}

// syncRecorded syncs a directory from the source its own instance file records, so a synced
// directory stays usable after the registry is gone.
func (a *app) syncRecorded(cmd *cobra.Command, req syncRequest) (syncResult, error) {
	se, err := a.syncEnv()
	if err != nil {
		return syncResult{}, err
	}
	req.Reason = cmd.Name()
	res, err := sync.Recorded(cmd.Context(), se, req.request())
	return a.synced(res, req, err)
}

func (a *app) loadLocal(dir string) (*local.File, error) {
	se, err := a.syncEnv()
	if err != nil {
		return nil, err
	}
	return sync.LoadLocal(se, dir)
}
