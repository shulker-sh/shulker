package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/andrewmast/shulker/internal/lock"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/player"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/spf13/cobra"
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
			return a.printer.Emit(results, func(w io.Writer) {
				if len(results) == 0 {
					fmt.Fprintln(w, "no players to check")
					return
				}
				for _, r := range results {
					fmt.Fprintln(w, describePlayer(r))
				}
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "check every player in the manifest")
	return cmd
}

func describePlayer(r player.Result) string {
	line := fmt.Sprintf("%-10s %-16s %s", r.State, r.Name, r.UUID)
	switch r.State {
	case player.Renamed:
		line += "  (was " + r.Previous + ")"
	case player.Reassigned:
		line += "  (was " + r.Previous + ")"
	case player.Unknown:
		line = fmt.Sprintf("%-10s %s", r.State, r.Input)
		if len(r.Candidates) > 0 {
			line += "  (did you mean " + strings.Join(r.Candidates, ", ") + "?)"
		}
	}
	return strings.TrimRight(line, " ")
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

func (a *app) syncPlayers(ctx context.Context, p *project.Project, mode player.Mode, acceptChange bool) error {
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
	return a.savePlayers(p, player.Apply(results, p.Lock.Players, acceptChange))
}

func (a *app) savePlayers(p *project.Project, next []lock.Player) error {
	if reflect.DeepEqual(next, p.Lock.Players) {
		return nil
	}
	p.Lock.Players = next
	return p.Lock.Save(p.LockPath())
}
