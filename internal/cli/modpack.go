package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

const typeFlagUsage = "entry type: mod, modpack, resourcepack, shader"

var contentTypes = []string{manifest.TypeMod, manifest.TypeModpack, manifest.TypeResourcePack, manifest.TypeShader}

// typeFlags lists the flags each type accepts. The plain verbs register every
// flag and refuse the ones the chosen type has no use for, so a group command
// and its `--type` spelling take the same flags.
var typeFlags = map[string][]string{
	manifest.TypeMod:          {"side", "channel", "pin", "provider", "as", "with-deps"},
	manifest.TypeModpack:      {"ref", "as", "unlocked"},
	manifest.TypeResourcePack: {"channel", "pin", "provider", "as"},
	manifest.TypeShader:       {"channel", "pin", "provider", "as"},
}

var allTypeFlags = []string{"as", "channel", "pin", "provider", "ref", "side", "unlocked", "with-deps"}

// inferredFlags are the flags an entry may take while its type is still the
// provider's to settle. A modpack is never inferred — it takes a source, not a
// provider slug — so the modpack's own flags need --type before they apply.
var inferredFlags = []string{"side", "channel", "pin", "provider", "as", "with-deps"}

func (a *app) typeGroupCmds() []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(contentTypes))
	for _, kind := range contentTypes {
		cmd := &cobra.Command{Use: kind, Short: groupShort(kind)}
		cmd.AddCommand(a.addCmdFor(kind), a.removeCmdFor(kind), a.listCmdFor(kind))
		cmds = append(cmds, cmd)
	}
	return cmds
}

func groupShort(kind string) string {
	if kind == manifest.TypeModpack {
		return "Manage modpacks whose mods and overrides merge into this project"
	}
	return "Manage the project's " + kind + "s"
}

func applies(kind, flag string) bool {
	return kind == "" || slices.Contains(typeFlags[kind], flag)
}

// chooseType settles which type a verb is working on and refuses the flags that
// type has no use for. An empty fallback means the verb spans every type.
func chooseType(cmd *cobra.Command, kind, typ, fallback string) (string, error) {
	chosen := kind
	if chosen == "" {
		chosen = typ
	}
	if chosen == "" {
		chosen = fallback
	}
	if chosen == "" {
		if wrong := changedFlags(cmd, inferredFlags); len(wrong) > 0 {
			return "", out.Errorf("usage", "%s only applies to a modpack; pass `--type modpack`", strings.Join(wrong, " and "))
		}
		return "", nil
	}
	if !slices.Contains(contentTypes, chosen) {
		e := out.Errorf("usage", "--type takes one of %s, not %q", strings.Join(contentTypes, ", "), typ)
		e.Candidates, e.Given, e.Flag = contentTypes, typ, "--type"
		return "", e
	}
	if wrong := changedFlags(cmd, typeFlags[chosen]); len(wrong) > 0 {
		return "", out.Errorf("usage", "%s doesn't apply to a %s", strings.Join(wrong, " and "), chosen)
	}
	return chosen, nil
}

// changedFlags lists the type flags the command was given that aren't allowed.
func changedFlags(cmd *cobra.Command, allowed []string) []string {
	var wrong []string
	for _, name := range allTypeFlags {
		if slices.Contains(allowed, name) {
			continue
		}
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			wrong = append(wrong, "--"+name)
		}
	}
	return wrong
}

func unsupportedType(kind string) error {
	return out.Errorf("requires-unsupported", "%s entries aren't supported yet", kind)
}

func (a *app) addModpacks(cmd *cobra.Command, sources []string, as, ref string, unlocked bool) error {
	if as != "" && len(sources) > 1 {
		return out.Errorf("usage", "--as applies to a single modpack")
	}
	return a.relock(cmd, func(p *project.Project, r *resolve.Resolver) (string, error) {
		for _, source := range sources {
			entry := manifest.Require{Source: source, Ref: ref}
			if unlocked {
				no := false
				entry.Locked = &no
			}
			for _, existing := range p.Manifest.Modpacks() {
				if existing.Source == source {
					return "", out.Errorf("modpack-exists", "modpack %s is already in the manifest", source)
				}
			}
			key := as
			if key == "" {
				derived, err := pack.Key(source)
				if err != nil {
					return "", err
				}
				key = derived
			}
			if held, taken := p.Manifest.Requires[key]; taken {
				return "", manifest.KeyTaken(key, held.Kind(), manifest.TypeModpack)
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
		}
		return "", nil
	})
}
