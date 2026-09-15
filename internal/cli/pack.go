package cli

import (
	"maps"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) packCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Manage packs whose mods and overrides merge into this project",
	}
	cmd.AddCommand(a.packAddCmd(), a.packRemoveCmd(), a.packListCmd())
	return cmd
}

func (a *app) packAddCmd() *cobra.Command {
	var entry manifest.Require
	var name string
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add a pack from a local path, git URL, or raw manifest URL",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entry.Source = args[0]
			return a.relock(cmd, func(p *project.Project, r *resolve.Resolver) (string, error) {
				for _, existing := range p.Manifest.Modpacks() {
					if existing.Source == entry.Source {
						return "", out.Errorf("pack-exists", "pack %s is already in the manifest", entry.Source)
					}
				}
				key, err := pack.Key(entry.Source, name)
				if err != nil {
					return "", err
				}
				if _, taken := p.Manifest.Requires[key]; taken {
					return "", out.Errorf("pack-name", "requires already has %s; pass --name to pick another name", key)
				}
				store, err := a.packStore(p)
				if err != nil {
					return "", err
				}
				loaded, err := store.Resolve(cmd.Context(), key, entry)
				if err != nil {
					return "", err
				}
				if err := r.AddPack(cmd.Context(), loaded); err != nil {
					return "", err
				}
				p.Manifest.Requires[key] = entry
				return "", nil
			})
		},
	}
	cmd.Flags().StringVar(&entry.Ref, "ref", "", "branch, tag, or commit for git sources")
	cmd.Flags().StringVar(&name, "name", "", "name used in requires, messages and requiredBy (default: derived from the source)")
	return cmd
}

func (a *app) packRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a pack and prune the mods only it provided",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				return "", r.RemovePack(args[0])
			})
		},
	}
}

func (a *app) packListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List packs with their locked ref and whether a local pack has changed",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			store, err := a.packStore(p)
			if err != nil {
				return err
			}
			res := []pack.Status{}
			modpacks := p.Manifest.Modpacks()
			for _, name := range slices.Sorted(maps.Keys(modpacks)) {
				var pinned lock.Modpack
				locked := false
				if p.Lock != nil {
					pinned, locked = p.Lock.Modpacks[name]
				}
				st, err := store.Status(name, modpacks[name], pinned, locked)
				if err != nil {
					return err
				}
				res = append(res, st)
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				if len(res) == 0 {
					l.Info("No packs yet; add one with `shulker pack add <source>`.")
					return
				}
				var items []out.Item
				for _, st := range res {
					aside := []string{string(st.Kind), st.State}
					if st.Pin != "" {
						aside = append(aside, "pinned "+st.Pin)
					}
					if st.Ref != "" {
						aside = append(aside, "ref "+st.Ref)
					}
					items = append(items, out.Item{Kind: out.Note, Name: st.Name, Text: l.T.Grey(st.Source), Aside: aside})
				}
				l.Items(items...)
			})
		},
	}
}
