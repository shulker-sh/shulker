package cli

import (
	"context"
	"fmt"
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
	manifest.TypeModpack:      {"ref", "as", "unlocked", "no-auto-update"},
	manifest.TypeResourcePack: {"channel", "pin", "provider", "as"},
	manifest.TypeShader:       {"channel", "pin", "provider", "as"},
}

var allTypeFlags = []string{"as", "channel", "no-auto-update", "pin", "provider", "ref", "side", "unlocked", "with-deps"}

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
			e := out.Errorf("usage", "%s only applies to a modpack", strings.Join(wrong, " and "))
			e.Help = "pass `--type modpack`"
			return "", e
		}
		return "", nil
	}
	if err := checkType(chosen); err != nil {
		return "", err
	}
	if wrong := changedFlags(cmd, typeFlags[chosen]); len(wrong) > 0 {
		return "", out.Errorf("usage", "%s doesn't apply to a %s", strings.Join(wrong, " and "), chosen)
	}
	return chosen, nil
}

// checkType refuses a type that names none of the entry types.
func checkType(typ string) error {
	if typ == "" || slices.Contains(contentTypes, typ) {
		return nil
	}
	e := out.Errorf("usage", "--type takes one of %s, not %q", strings.Join(contentTypes, ", "), typ)
	e.Candidates, e.Given, e.Flag = contentTypes, typ, "--type"
	return e
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

func (a *app) addModpacks(cmd *cobra.Command, sources []string, as, ref string, unlocked, noAutoUpdate bool) error {
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
			if noAutoUpdate {
				no := false
				entry.AutoUpdate = &no
			}
			if err := a.addPackEntry(cmd.Context(), p, r, source, as, entry); err != nil {
				return "", err
			}
		}
		return "", nil
	})
}

// addPackEntry resolves one modpack source and puts it in the manifest under the key as names,
// or the name the pack's own manifest carries.
func (a *app) addPackEntry(ctx context.Context, p *project.Project, r *resolve.Resolver, source, as string, entry manifest.Require) error {
	for _, existing := range p.Manifest.Modpacks() {
		if existing.Source == source {
			return out.Errorf("modpack-exists", "modpack %s is already in the manifest", source)
		}
	}
	store, err := a.packStore(p)
	if err != nil {
		return err
	}
	loaded, err := store.Resolve(ctx, source, entry)
	if err != nil {
		return err
	}
	key := as
	if key == "" {
		key = loaded.Manifest.Name
	}
	if held, taken := p.Manifest.Requires[key]; taken {
		return manifest.KeyTaken(key, held.Kind(), manifest.TypeModpack)
	}
	loaded.Name = key
	err = r.AddPack(ctx, loaded)
	if a.offerUnlock(err, loaded, r.Lock.Minecraft) {
		unlock, askErr := a.askYes(fmt.Sprintf("Unlock %s and resolve its mods for Minecraft %s?", key, r.Lock.Minecraft))
		if askErr != nil {
			return askErr
		}
		if unlock {
			no := false
			entry.Locked = &no
			if loaded, err = store.Resolve(ctx, source, entry); err != nil {
				return err
			}
			loaded.Name = key
			err = r.AddPack(ctx, loaded)
		}
	}
	if err != nil {
		return err
	}
	p.Manifest.Requires[key] = entry
	return nil
}

// offerUnlock reports whether a modpack refused for its platform is the one refusal unlocking
// answers: locked, and built for another Minecraft than the project's.
func (a *app) offerUnlock(err error, l *pack.Loaded, minecraft string) bool {
	return out.CodeOf(err) == "modpack-mismatch" && a.canPick() && l.UsesLock && l.Lock != nil && minecraft != "" && l.Lock.Minecraft != minecraft
}
