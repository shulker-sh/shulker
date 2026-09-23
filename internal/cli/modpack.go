package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

const typeFlagUsage = "entry type: mod, modpack, resourcepack, shader, datapack"

var contentTypes = []string{manifest.TypeMod, manifest.TypeModpack, manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack}

// typeFlags lists the flags each type accepts. The plain verbs register every
// flag and refuse the ones the chosen type has no use for, so a group command
// and its `--type` spelling take the same flags.
var typeFlags = map[string][]string{
	manifest.TypeMod:          {"side", "channel", "pin", "provider", "as", "with-deps"},
	manifest.TypeModpack:      {"ref", "as", "unlocked", "no-auto-update", "channel", "pin", "provider"},
	manifest.TypeResourcePack: {"channel", "pin", "provider", "as"},
	manifest.TypeShader:       {"channel", "pin", "provider", "as"},
	manifest.TypeDatapack:     {"side", "channel", "pin", "provider", "as", "resourcepack"},
}

var allTypeFlags = []string{"as", "channel", "no-auto-update", "pin", "provider", "ref", "resourcepack", "side", "unlocked", "with-deps"}

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

func (a *app) addModpacks(cmd *cobra.Command, sources []string, opts resolve.AddOptions, ref string, unlocked, noAutoUpdate bool) error {
	as := opts.As
	if as != "" && len(sources) > 1 {
		return out.Errorf("usage", "--as applies to a single modpack")
	}
	if opts.Pin != "" && len(sources) > 1 {
		return out.Errorf("usage", "--pin applies to a single modpack")
	}
	urls, err := providerURLs(sources)
	if err != nil {
		return err
	}
	isHosted := func(source string) bool {
		_, ok := urls[source]
		return ok || a.isSlug(source)
	}
	hosted := slices.ContainsFunc(sources, isHosted)
	if err := refuseModpackFlags(cmd, hosted, !slices.ContainsFunc(sources, func(s string) bool { return !isHosted(s) })); err != nil {
		return err
	}
	return a.relock(cmd, relockPlan{}, func(p *project.Project, r *resolve.Resolver) (string, error) {
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
			if a.isSlug(source) {
				if err := r.Add(cmd.Context(), source, opts); err != nil {
					return "", err
				}
				continue
			}
			entry := manifest.Require{Source: source, Ref: ref}
			if unlocked {
				no := false
				entry.Locked = &no
			}
			if noAutoUpdate {
				no := false
				entry.AutoUpdate = &no
			}
			if path := a.localPath(source); resolve.IsLocalPath(path) {
				if err := a.addArchiveEntry(cmd.Context(), p, r, path, as, entry); err != nil {
					return "", err
				}
				continue
			}
			if err := a.addPackEntry(cmd.Context(), p, r, source, as, entry); err != nil {
				return "", err
			}
		}
		return "", nil
	})
}

// isSlug reports whether a modpack argument names a project on a provider rather than a source: it
// is no URL, no path and no directory that exists.
func (a *app) isSlug(arg string) bool {
	if pack.Classify(arg) != pack.Local || strings.ContainsAny(arg, `/\`) || arg == "." || arg == ".." || resolve.IsLocalPath(a.localPath(arg)) {
		return false
	}
	dir := arg
	if a.dir != "" {
		dir = filepath.Join(a.dir, arg)
	}
	_, err := os.Stat(dir)
	return err != nil
}

// refuseModpackFlags refuses the flags that don't apply to the modpacks being added, each with its
// reason: a hosted modpack is a provider version, and a source or archive is not.
func refuseModpackFlags(cmd *cobra.Command, hosted, onlyHosted bool) error {
	given := func(name string) bool { return cmd.Flags().Changed(name) }
	if hosted {
		for _, flag := range []struct{ name, reason string }{
			{"ref", "a ref names a git commit, and a modpack from a provider is a provider version"},
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

// lockHostedEntry locks the hosted modpack entry and puts it in the manifest under key, in place of
// the modpack already loaded there.
func (a *app) lockHostedEntry(ctx context.Context, p *project.Project, r *resolve.Resolver, key string, entry manifest.Require) error {
	store, err := a.packStore(p)
	if err != nil {
		return err
	}
	loaded, err := store.Resolve(ctx, key, entry)
	if err != nil {
		return err
	}
	if channel := loaded.Pin.Channel; channel != "release" {
		entry.Channel = channel
	}
	i := slices.IndexFunc(r.Packs, func(l *pack.Loaded) bool { return l.Name == key })
	if i < 0 {
		return a.addLoadedPack(ctx, p, r, store, key, key, loaded, entry)
	}
	packs := slices.Clone(r.Packs)
	packs[i] = loaded
	if err := r.RefreshPacks(packs); err != nil {
		return err
	}
	replacePacks(p, packs)
	p.Manifest.Requires[key] = entry
	return nil
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
	return a.addLoadedPack(ctx, p, r, store, source, key, loaded, entry)
}

// addArchiveEntry adds the modpack archive at path, under the key as names or the file's stem. An
// archive outside the project, or in a folder whose files something else owns, is copied into
// manifest.FilesDir, and adding the same file again refreshes that copy and relocks it.
func (a *app) addArchiveEntry(ctx context.Context, p *project.Project, r *resolve.Resolver, path, as string, entry manifest.Require) error {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return out.Errorf("file-not-found", "%s is not a file", path)
	}
	rel, copied, err := r.ProjectPath(path)
	if err != nil {
		return err
	}
	key := as
	if key == "" {
		key = resolve.StemKey(path)
	}
	if !manifest.IsValidKey(key) {
		e := out.Errorf("usage", "%s can't be a requires key", key)
		e.Help = "pass `--as <key>` to give this modpack one"
		return e
	}
	held, taken := p.Manifest.Requires[key]
	isReadded := taken && held.Kind() == manifest.TypeModpack && held.File == rel
	if taken && !isReadded {
		return manifest.KeyTaken(key, held.Kind(), manifest.TypeModpack)
	}
	for name, existing := range p.Manifest.Modpacks() {
		if existing.File == rel && name != key {
			return out.Errorf("modpack-exists", "modpack %s is already in the manifest as %s", rel, name)
		}
	}
	if err := pack.CheckArchive(key, path); err != nil {
		return err
	}
	entry.Source, entry.Type, entry.File = "", manifest.TypeModpack, rel
	if isReadded {
		if entry.Locked == nil {
			entry.Locked = held.Locked
		}
		if entry.AutoUpdate == nil {
			entry.AutoUpdate = held.AutoUpdate
		}
	}
	if copied {
		if err := r.CopyIn(path, rel, isReadded); err != nil {
			return err
		}
	}
	store, err := a.packStore(p)
	if err != nil {
		return err
	}
	loaded, err := store.Resolve(ctx, key, entry)
	if err != nil {
		return err
	}
	if isReadded {
		if err := r.RemovePack(key); err != nil {
			return err
		}
	}
	return a.addLoadedPack(ctx, p, r, store, key, key, loaded, entry)
}

// addLoadedPack adds a resolved modpack under key, offering to unlock one built for another
// Minecraft, which resolves it again as name.
func (a *app) addLoadedPack(ctx context.Context, p *project.Project, r *resolve.Resolver, store *pack.Store, name, key string, loaded *pack.Loaded, entry manifest.Require) error {
	a.warnFor(key, true, loaded.Warnings)
	err := r.AddPack(ctx, loaded)
	if a.offerUnlock(err, loaded, r.Lock.Minecraft) {
		unlock, askErr := a.askYes(fmt.Sprintf("Unlock %s and resolve its mods for Minecraft %s?", key, r.Lock.Minecraft))
		if askErr != nil {
			return askErr
		}
		if unlock {
			no := false
			entry.Locked = &no
			if loaded, err = store.Resolve(ctx, name, entry); err != nil {
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
	return out.CodeOf(err) == "modpack-mismatch" && a.canPick() && l.Kind != pack.Hosted && l.UsesLock && l.Lock != nil && minecraft != "" && l.Lock.Minecraft != minecraft
}
