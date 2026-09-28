package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

const typeFlagUsage = "entry type: mod, modpack, resourcepack, shader, datapack"

var contentTypes = []string{manifest.TypeMod, manifest.TypeModpack, manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack}

// typeFlags lists the flags each type accepts. The plain verbs register every
// flag and refuse the ones the chosen type has no use for, so a group command
// and its `--type` spelling take the same flags.
var typeFlags = map[string][]string{
	manifest.TypeMod:          {"side", "channel", "pin", "provider", "as", "with-deps", "skip-missing", "yes"},
	manifest.TypeModpack:      {"ref", "path", "as", "unlocked", "no-auto-update", "channel", "pin", "provider", "yes"},
	manifest.TypeResourcePack: {"channel", "pin", "provider", "as", "skip-missing"},
	manifest.TypeShader:       {"channel", "pin", "provider", "as", "skip-missing"},
	manifest.TypeDatapack:     {"side", "channel", "pin", "provider", "as", "resourcepack", "skip-missing"},
}

var allTypeFlags = []string{"as", "channel", "no-auto-update", "path", "pin", "provider", "ref", "resourcepack", "side", "skip-missing", "unlocked", "with-deps", "yes"}

// inferredFlags are the flags an entry may take while its type is still the
// provider's to settle. A modpack is never inferred — it takes a source, not a
// provider slug — so the modpack's own flags need --type before they apply.
var inferredFlags = []string{"side", "channel", "pin", "provider", "as", "with-deps", "skip-missing", "yes"}

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

func (a *app) addModpacks(cmd *cobra.Command, sources []string, opts resolve.AddOptions, at modpack.At, unlocked, noAutoUpdate bool) error {
	as := opts.As
	if as != "" && len(sources) > 1 {
		return out.Errorf("usage", "--as applies to a single modpack")
	}
	if opts.Pin != "" && len(sources) > 1 {
		return out.Errorf("usage", "--pin applies to a single modpack")
	}
	urls, err := a.providerURLs(sources)
	if err != nil {
		return err
	}
	isHosted := func(source string) bool {
		_, ok := urls[source]
		return ok || resolve.IsSlug(source, a.dir)
	}
	hosted := slices.ContainsFunc(sources, isHosted)
	if err := refuseModpackFlags(cmd, hosted, !slices.ContainsFunc(sources, func(s string) bool { return !isHosted(s) })); err != nil {
		return err
	}
	return a.relock(cmd, relockPlan{isFetched: true}, func(p *project.Project, r *resolve.Resolver) (string, error) {
		store, err := a.packStore(p)
		if err != nil {
			return "", err
		}
		for _, source := range sources {
			if u, ok := urls[source]; ok {
				slug, add, err := r.FromURL(cmd.Context(), u, opts)
				if err != nil {
					return "", err
				}
				if err := r.Add(cmd.Context(), slug, add); err != nil {
					return "", err
				}
				continue
			}
			if resolve.IsSlug(source, a.dir) {
				if err := r.Add(cmd.Context(), source, opts); err != nil {
					return "", err
				}
				continue
			}
			if err := modpack.CheckPath(at.Path, modpack.Classify(source)); err != nil {
				return "", err
			}
			entry := manifest.Require{Source: source, Ref: at.Ref, Path: at.Path}
			if unlocked {
				no := false
				entry.Locked = &no
			}
			if noAutoUpdate {
				no := false
				entry.AutoUpdate = &no
			}
			if path := a.localPath(source); resolve.IsLocalPath(path) {
				if err := r.AddArchive(cmd.Context(), store, path, as, entry); err != nil {
					return "", err
				}
				continue
			}
			if err := r.AddPackSource(cmd.Context(), store, source, as, entry); err != nil {
				return "", err
			}
		}
		return "", nil
	})
}

// refuseModpackFlags refuses the flags that don't apply to the modpacks being added, each with its
// reason: a hosted modpack is a provider version, and a source or archive is not.
func refuseModpackFlags(cmd *cobra.Command, hosted, onlyHosted bool) error {
	given := func(name string) bool { return cmd.Flags().Changed(name) }
	if hosted {
		for _, flag := range []struct{ name, reason string }{
			{"ref", "a ref names a git commit, and a modpack from a provider is a provider version"},
			{"path", "a path names a folder of a git repository, and a modpack from a provider is an archive"},
			{"unlocked", "a modpack from a provider is always locked: its archive pins every file"},
			{"no-auto-update", "a modpack from a provider never auto-updates: `shulker update` moves it and `shulker pin` holds it"},
		} {
			if given(flag.name) {
				return out.Errorf("usage", "--%s doesn't apply to a modpack from a provider: %s", flag.name, flag.reason)
			}
		}
	}
	if !onlyHosted {
		for _, name := range []string{"pin", "channel", "provider"} {
			if given(name) {
				return out.Errorf("usage", "--%s only applies to a modpack from a provider, not a source or archive", name)
			}
		}
	}
	return nil
}
