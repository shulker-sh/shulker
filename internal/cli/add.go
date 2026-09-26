package cli

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) addCmd() *cobra.Command { return a.addCmdFor("") }

func (a *app) addCmdFor(kind string) *cobra.Command {
	var opts resolve.AddOptions
	var typ, as string
	var at modpack.At
	var unlocked, noAutoUpdate bool
	cmd := &cobra.Command{
		Use:         "add " + addArgs(kind),
		Annotations: acts(),
		Short:       addShort(kind),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && a.canPick() {
				return nil
			}
			return minimumArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			urls, err := a.providerURLs(args)
			if err != nil {
				return err
			}
			fallback := ""
			if opts.ResourcePack {
				fallback = manifest.TypeDatapack
			}
			if kind == "" && typ == "" && len(args) > 0 && !slices.ContainsFunc(args, func(arg string) bool { return !a.isArchive(arg) }) {
				fallback = manifest.TypeModpack
			}
			chosen, err := chooseType(cmd, kind, typ, fallback)
			if err != nil {
				return err
			}
			// from is the provider each project picked from a search was found on.
			var from map[string]string
			if len(args) == 0 {
				if chosen == manifest.TypeModpack {
					return minimumArgs(1)(cmd, args)
				}
				if args, from, err = a.askAdd(cmd, chosen, opts.Provider); err != nil {
					return err
				}
			}
			if as != "" && !manifest.IsValidKey(as) {
				return out.Errorf("usage", "--as takes up to 64 lowercase letters, digits, dots, dashes and underscores, starting with a letter or digit, not %q", as)
			}
			switch chosen {
			case manifest.TypeModpack:
				opts.As = as
				return a.addModpacks(cmd, args, opts, at, unlocked, noAutoUpdate)
			case "", manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack:
				// An empty type is settled by the provider during resolution.
			default:
				return unsupportedType(chosen)
			}
			if opts.Pin != "" && len(args) > 1 {
				return out.Errorf("usage", "--pin applies to a single mod")
			}
			if as != "" && len(args) > 1 {
				return out.Errorf("usage", "--as applies to a single mod")
			}
			opts.As, opts.Type = as, chosen
			for i, arg := range args {
				if _, ok := urls[arg]; !ok {
					args[i] = a.localPath(arg)
				}
			}
			if err := checkFlagValues(
				flagValue{"side", opts.Side, []string{"client", "server", "both"}},
				flagValue{"channel", opts.Channel, []string{"release", "beta", "alpha"}},
				flagValue{"provider", opts.Provider, manifest.DefaultProviders},
			); err != nil {
				return err
			}
			return a.relock(cmd, relockPlan{isFetched: true}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
				for _, arg := range args {
					add, slug := opts, arg
					if name, ok := from[arg]; ok {
						add.Provider = name
					}
					if u, ok := urls[arg]; ok {
						var err error
						if slug, add, err = r.FromURL(cmd.Context(), u, add); err != nil {
							return "", err
						}
					}
					if err := a.addAsking(cmd.Context(), r, slug, add); err != nil {
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
	if applies(kind, "resourcepack") {
		cmd.Flags().BoolVar(&opts.ResourcePack, "resourcepack", false, "also place the datapack in resourcepacks/, for one that carries assets/")
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
		cmd.Flags().StringVar(&at.Ref, "ref", "", "branch, tag, or commit for git sources")
	}
	if applies(kind, "path") {
		cmd.Flags().StringVar(&at.Path, "path", "", "folder of a git source's repository that holds the modpack's shulker.json (default: the root)")
	}
	if applies(kind, "unlocked") {
		cmd.Flags().BoolVar(&unlocked, "unlocked", false, "resolve the modpack's mods here instead of copying the versions its lock pins")
	}
	if applies(kind, "no-auto-update") {
		cmd.Flags().BoolVar(&noAutoUpdate, "no-auto-update", false, "keep the modpack at its locked version on `shulker sync`; `shulker update` still moves it")
	}
	if applies(kind, "with-deps") {
		cmd.Flags().BoolVar(&opts.WithDeps, "with-deps", false, "move dependency versions the lock holds when a mod being added needs another")
	}
	if applies(kind, "as") {
		cmd.Flags().StringVar(&as, "as", "", "key used in requires, messages and requiredBy (default: a mod's jar id, a pack or hosted modpack's provider slug, a modpack archive file's name, the name in a modpack's manifest)")
	}
	return cmd
}

// flagValue is a flag's value and the values it takes.
type flagValue struct {
	name, value string
	allowed     []string
}

// checkFlagValues refuses a flag given a value it doesn't take.
func checkFlagValues(flags ...flagValue) error {
	for _, flag := range flags {
		if flag.value != "" && !slices.Contains(flag.allowed, flag.value) {
			return out.Errorf("usage", "--%s takes one of %s, not %q", flag.name, strings.Join(flag.allowed, ", "), flag.value)
		}
	}
	return nil
}

// providerURLs reads the arguments that are provider URLs.
func (a *app) providerURLs(args []string) (map[string]provider.Ref, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	urls := map[string]provider.Ref{}
	for _, arg := range args {
		u, ok, err := d.Providers.ParseURL(arg)
		if err != nil {
			return nil, err
		}
		if ok {
			urls[arg] = u
		}
	}
	return urls, nil
}

// isArchive reports whether an add argument is a modpack archive, which a bare add takes as a
// modpack: an .mrpack by its name, and a zip by holding a CurseForge manifest.
func (a *app) isArchive(arg string) bool {
	if d, err := a.deps(); err == nil {
		if _, ok, _ := d.Providers.ParseURL(arg); ok {
			return false
		}
	}
	if _, ok := packarchive.NamedFormat(arg); ok {
		return true
	}
	return packarchive.HasArchiveExtension(arg) && packarchive.IsArchive(a.localPath(arg))
}

// localPath is an argument naming a local file or folder made absolute, against -C when it is
// given, and any other argument as it came.
func (a *app) localPath(arg string) string {
	path := arg
	if !filepath.IsAbs(path) && a.dir != "" {
		path = filepath.Join(a.dir, path)
	}
	if !resolve.IsLocalPath(path) && !resolve.IsLocalFolder(path) {
		return arg
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return arg
}

func addArgs(kind string) string {
	switch kind {
	case manifest.TypeModpack:
		return "<source|slug>..."
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
		return "Add a modpack from a local path, git URL, raw manifest URL, archive, or provider slug"
	case "":
		return "Add mods or modpacks to the manifest and lock"
	}
	return "Add " + kind + "s to the manifest and lock"
}
