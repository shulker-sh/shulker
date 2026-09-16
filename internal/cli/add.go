package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) addCmd() *cobra.Command { return a.addCmdFor("") }

func (a *app) addCmdFor(kind string) *cobra.Command {
	var opts resolve.AddOptions
	var typ, as, ref string
	var unlocked bool
	cmd := &cobra.Command{
		Use:   "add " + addArgs(kind),
		Short: addShort(kind),
		Args:  minimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chosen, err := chooseType(cmd, kind, typ, manifest.TypeMod)
			if err != nil {
				return err
			}
			if as != "" && !manifest.ValidKey(as) {
				return out.Errorf("usage", "--as takes up to 64 lowercase letters, digits, dots, dashes and underscores, starting with a letter or digit, not %q", as)
			}
			switch chosen {
			case manifest.TypeModpack:
				return a.addModpacks(cmd, args, as, ref, unlocked)
			case manifest.TypeMod:
			default:
				return unsupportedType(chosen)
			}
			if opts.Pin != "" && len(args) > 1 {
				return fmt.Errorf("--pin applies to a single mod")
			}
			if as != "" && len(args) > 1 {
				return out.Errorf("usage", "--as applies to a single mod")
			}
			opts.As = as
			for _, flag := range []struct {
				name, value string
				allowed     []string
			}{
				{"side", opts.Side, []string{"client", "server", "both"}},
				{"channel", opts.Channel, []string{"release", "beta", "alpha"}},
				{"provider", opts.Provider, []string{"modrinth", "curseforge"}},
			} {
				if flag.value != "" && !slices.Contains(flag.allowed, flag.value) {
					return out.Errorf("usage", "--%s takes one of %s, not %q", flag.name, strings.Join(flag.allowed, ", "), flag.value)
				}
			}
			return a.relock(cmd, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				for _, slug := range args {
					if err := r.Add(cmd.Context(), slug, opts); err != nil {
						return "", err
					}
				}
				return "", nil
			})
		},
	}
	if kind == "" {
		cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	}
	if applies(kind, "side") {
		cmd.Flags().StringVar(&opts.Side, "side", "", "override side: client, server, both")
	}
	if applies(kind, "channel") {
		cmd.Flags().StringVar(&opts.Channel, "channel", "", "least stable channel accepted: release, beta, alpha")
	}
	if applies(kind, "pin") {
		cmd.Flags().StringVar(&opts.Pin, "pin", "", "pin to a provider version id")
	}
	if applies(kind, "provider") {
		cmd.Flags().StringVar(&opts.Provider, "provider", "", "provider to use for this mod")
	}
	if applies(kind, "ref") {
		cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit for git sources")
	}
	if applies(kind, "unlocked") {
		cmd.Flags().BoolVar(&unlocked, "unlocked", false, "resolve the modpack's mods here instead of copying the versions its lock pins")
	}
	if applies(kind, "as") {
		cmd.Flags().StringVar(&as, "as", "", "key used in requires, messages and requiredBy (default: a mod's jar id, a modpack source's name)")
	}
	return cmd
}

func addArgs(kind string) string {
	switch kind {
	case manifest.TypeModpack:
		return "<source>..."
	case "":
		return "<mod|source>..."
	}
	return "<" + kind + ">..."
}

func addShort(kind string) string {
	switch kind {
	case manifest.TypeMod:
		return "Add mods to the manifest and lock"
	case manifest.TypeModpack:
		return "Add a modpack from a local path, git URL, or raw manifest URL"
	case "":
		return "Add mods or modpacks to the manifest and lock"
	}
	return "Add " + kind + "s to the manifest and lock"
}
