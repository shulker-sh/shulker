package cli

import (
	"context"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
)

func (a *app) playerCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "player [name|uuid]... | --all",
		Short: "Check player names and uuids against Mojang and the lock",
		Long:  "Reports one state per player: ok, renamed (same uuid, new name), reassigned (same name, different account), or unknown. Pass names or uuids, or --all for every player in the manifest. Renames are recorded in the lock; reassignments wait for `build --accept-player-change`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) > 0) {
				return out.Errorf("usage", "pass player names or uuids, or --all for every player in the manifest")
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			var refs []player.Ref
			if all {
				refs = manifestPlayerRefs(p.Manifest)
			} else {
				for _, arg := range args {
					ref, err := player.ParseRef(arg)
					if err != nil {
						return err
					}
					refs = append(refs, ref)
				}
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			results, err := d.players.Sync(cmd.Context(), refs, p.Lock.Players, player.Recheck)
			if err != nil {
				return err
			}
			if err := a.savePlayers(p, player.Update(results, p.Lock.Players)); err != nil {
				return err
			}
			return a.printer.Emit(results, func(l *out.Lines) {
				if len(results) == 0 {
					l.Info("No players to check.")
					return
				}
				var items []out.Item
				for _, r := range results {
					items = append(items, describePlayer(r))
				}
				l.Items(items...)
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "check every player in the manifest")
	return cmd
}

func describePlayer(r player.Result) out.Item {
	it := out.Item{Kind: out.Good, Name: r.Name, Version: r.UUID}
	switch r.State {
	case player.Renamed, player.Reassigned:
		it.Kind = out.Change
		it.Aside = []string{string(r.State), "was " + r.Previous}
	case player.Unknown:
		it.Kind = out.Drop
		it.Name, it.Version, it.Aside = r.Input, "", []string{string(r.State)}
		if len(r.Candidates) > 0 {
			it.Aside = append(it.Aside, "did you mean "+strings.Join(r.Candidates, ", ")+"?")
		}
	}
	return it
}

func manifestPlayerRefs(m *manifest.Manifest) []player.Ref {
	var refs []player.Ref
	if m.Server == nil {
		return refs
	}
	for _, p := range m.Server.Players.All() {
		refs = append(refs, player.Ref{Name: p.Name, UUID: p.UUID})
	}
	return refs
}

func (a *app) syncPlayers(ctx context.Context, p *project.Project, mode player.Mode, acceptChange, persist bool) error {
	refs := manifestPlayerRefs(p.Manifest)
	if len(refs) == 0 && len(p.Lock.Players) == 0 {
		return nil
	}
	d, err := a.deps()
	if err != nil {
		return err
	}
	results, err := d.players.Sync(ctx, refs, p.Lock.Players, mode)
	if err != nil {
		return err
	}
	warnings, err := player.Policy(results, acceptChange)
	a.warn(warnings)
	if err != nil {
		return err
	}
	next := player.Apply(results, p.Lock.Players, acceptChange)
	if !persist {
		p.Lock.Players = next
		return nil
	}
	return a.savePlayers(p, next)
}

func (a *app) savePlayers(p *project.Project, next []lock.Player) error {
	if reflect.DeepEqual(next, p.Lock.Players) {
		return nil
	}
	p.Lock.Players = next
	return p.Lock.Save(p.LockPath())
}
