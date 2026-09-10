package cli

import (
	"fmt"
	"io"

	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/shulker-sh/shulker/internal/pack"
	"github.com/shulker-sh/shulker/internal/project"
	"github.com/shulker-sh/shulker/internal/resolve"
	"github.com/spf13/cobra"
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
	var entry manifest.Pack
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add a pack from a local path, git URL, or raw manifest URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entry.Source = args[0]
			return a.relock(cmd, func(p *project.Project, r *resolve.Resolver) (*resolve.Updated, string, error) {
				for _, existing := range p.Manifest.Packs {
					if existing.Source == entry.Source {
						return nil, "", out.Errorf("pack-exists", "pack %s is already in the manifest", entry.Source)
					}
				}
				if _, err := pack.Names(append(append([]manifest.Pack{}, p.Manifest.Packs...), entry)); err != nil {
					return nil, "", err
				}
				store, err := a.packStore(p)
				if err != nil {
					return nil, "", err
				}
				loaded, err := store.Resolve(cmd.Context(), entry)
				if err != nil {
					return nil, "", err
				}
				res, err := r.AddPack(cmd.Context(), loaded)
				if err != nil {
					return nil, "", err
				}
				p.Manifest.Packs = append(p.Manifest.Packs, entry)
				return res, "", nil
			})
		},
	}
	cmd.Flags().StringVar(&entry.Ref, "ref", "", "branch, tag, or commit for git sources")
	cmd.Flags().StringVar(&entry.Name, "name", "", "name used in messages and requiredBy (default: derived from the source)")
	return cmd
}

func (a *app) packRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a pack and prune the mods only it provided",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			r, err := a.resolver(cmd.Context(), p)
			if err != nil {
				return err
			}
			res, err := r.RemovePack(args[0])
			if err != nil {
				return err
			}
			if err := p.SaveManifest(); err != nil {
				return err
			}
			if err := p.SaveLock(); err != nil {
				return err
			}
			a.printer.LockStale = false
			return a.printer.Emit(res, func(w io.Writer) {
				fmt.Fprintf(w, "- pack %s\n", args[0])
				for _, id := range res.Pruned {
					fmt.Fprintf(w, "  pruned %s\n", id)
				}
				fmt.Fprintln(w, "Next: shulker install")
			})
		},
	}
}

func (a *app) packListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List packs with their locked ref and whether a local pack has changed",
		Args:    cobra.NoArgs,
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
			for _, entry := range p.Manifest.Packs {
				var pinned lock.Pack
				locked := false
				if p.Lock != nil {
					pinned, locked = p.Lock.Packs[entry.Source]
				}
				st, err := store.Status(entry, pinned, locked)
				if err != nil {
					return err
				}
				res = append(res, st)
			}
			return a.printer.Emit(res, func(w io.Writer) {
				if len(res) == 0 {
					fmt.Fprintln(w, "No packs.")
					return
				}
				for _, st := range res {
					fmt.Fprintf(w, "%s %s %s", st.Name, st.Kind, st.State)
					if st.Pin != "" {
						fmt.Fprintf(w, " %s", st.Pin)
					}
					fmt.Fprintf(w, " %s", st.Source)
					if st.Ref != "" {
						fmt.Fprintf(w, " (ref %s)", st.Ref)
					}
					fmt.Fprintln(w)
				}
			})
		},
	}
}
