package cli

import (
	"context"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

func (a *app) playerCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:         "player [name|uuid]... | --all",
		Annotations: reads(),
		Short:       "Check player names and uuids against Mojang and the lock",
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
				refs = player.RefsOf(p.Manifest)
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
			results, err := d.Players.Sync(cmd.Context(), refs, p.Lock.Players, player.Recheck)
			if err != nil {
				return err
			}
			if err := a.savePlayers(p, player.Update(results, p.Lock.Players)); err != nil {
				return err
			}
			return a.printer.Emit(results, func(l *out.Lines) {
				if len(results) == 0 {
					l.Info("No players to check")
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
	a.scopeFlags(cmd)
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

func (a *app) syncPlayers(ctx context.Context, p *project.Project, mode player.Mode, acceptChange, persist bool) error {
	se, err := a.syncEnv()
	if err != nil {
		return err
	}
	return sync.Players(ctx, se, p, mode, acceptChange, persist)
}

func (a *app) savePlayers(p *project.Project, next []lock.Player) error {
	if reflect.DeepEqual(next, p.Lock.Players) {
		return nil
	}
	p.Lock.Players = next
	return p.Lock.Save(p.LockPath())
}
