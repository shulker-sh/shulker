package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) removeCmd() *cobra.Command { return a.removeCmdFor("") }

func (a *app) removeCmdFor(kind string) *cobra.Command {
	var typ string
	cmd := &cobra.Command{
		Use:     "remove " + removeArgs(kind),
		Aliases: []string{"rm"},
		Short:   removeShort(kind),
		Args:    minimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chosen, err := chooseType(cmd, kind, typ, "")
			if err != nil {
				return err
			}
			if chosen == manifest.TypeResourcePack || chosen == manifest.TypeShader {
				return unsupportedType(chosen)
			}
			return a.relock(cmd, func(p *project.Project, r *resolve.Resolver) (string, error) {
				modpacks := p.Manifest.Modpacks()
				var mods []string
				for _, key := range args {
					_, isModpack := modpacks[key]
					switch {
					case chosen == manifest.TypeModpack || isModpack:
						if chosen == manifest.TypeMod {
							return "", out.Errorf("mod-not-found", "%s is a modpack; remove it with `shulker modpack remove %s`", key, key)
						}
						if err := r.RemovePack(key); err != nil {
							return "", err
						}
					default:
						mods = append(mods, key)
					}
				}
				if len(mods) == 0 {
					return "", nil
				}
				return "", r.Remove(mods)
			})
		},
	}
	if kind == "" {
		cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	}
	return cmd
}

func removeArgs(kind string) string {
	if kind == "" {
		return "<mod|modpack>..."
	}
	return "<" + kind + ">..."
}

func removeShort(kind string) string {
	switch kind {
	case manifest.TypeMod:
		return "Remove mods from the manifest and prune orphaned dependencies"
	case manifest.TypeModpack:
		return "Remove a modpack and prune the mods only it provided"
	case "":
		return "Remove mods or modpacks from the manifest and prune what only they provided"
	}
	return "Remove " + kind + "s from the manifest"
}
