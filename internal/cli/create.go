package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

type initResult struct {
	Name      string `json:"name"`
	Minecraft string `json:"minecraft"`
	Loader    string `json:"loader,omitempty"`
	Version   string `json:"loaderVersion,omitempty"`
	Java      int    `json:"java"`
	Side      string `json:"side"`
}

// initOptions is what init and create share: the flags, and what the wizard fills in for the
// ones init is not given.
type initOptions struct {
	name          string
	minecraft     string
	loaderName    string
	loaderVersion string
	side          string
	pack          string
	// aliases holds the bool flags that stand in for a value of --loader and --side, by the flag
	// they stand in for, so the wizard counts that flag as given by either spelling.
	aliases map[string]*aliasFlags
}

// aliasFlags are bool flags that each spell one value of a string flag, like --fabric for
// --loader fabric.
type aliasFlags struct {
	flag   string
	values []string
	set    map[string]*bool
	// chosen is the alias that named the value, once pick has run.
	chosen string
}

func newAliasFlags(cmd *cobra.Command, flag string, values []string) *aliasFlags {
	f := &aliasFlags{flag: flag, values: values, set: map[string]*bool{}}
	for _, v := range values {
		f.set[v] = cmd.Flags().Bool(v, false, "same as --"+flag+" "+v)
	}
	return f
}

// pick is the value the aliases and the flag they stand in for agree on, given the flag's own
// value; naming two different values is a usage error.
func (f *aliasFlags) pick(flags *pflag.FlagSet, current string) (string, error) {
	var spellings, named []string
	for _, v := range f.values {
		if *f.set[v] {
			spellings, named = append(spellings, "--"+v), append(named, v)
		}
	}
	if flags.Changed(f.flag) {
		spellings, named = append(spellings, "--"+f.flag+" "+current), append(named, current)
	}
	for _, v := range named {
		if v != named[0] {
			return "", out.Errorf("usage", "%s name different %ss", strings.Join(spellings, " and "), f.flag)
		}
	}
	if len(named) == 0 {
		return current, nil
	}
	f.chosen = spellings[0]
	return named[0], nil
}

func (a *app) createCmd() *cobra.Command {
	var opts initOptions
	cmd := &cobra.Command{
		Use:         "create",
		Annotations: acts(),
		Short:       "Create shulker.json and a lock in the current directory without asking",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.printer.NoInput = true
			dir, err := a.initDir()
			if err != nil {
				return err
			}
			if err := opts.settle(cmd.Flags()); err != nil {
				return err
			}
			return a.createProject(cmd, dir, &opts)
		},
	}
	initFlags(cmd, &opts)
	return cmd
}

func initFlags(cmd *cobra.Command, opts *initOptions) {
	cmd.Flags().StringVar(&opts.name, "name", "", "project name (default: directory name)")
	cmd.Flags().StringVar(&opts.minecraft, "minecraft", "", "Minecraft version or range (default: latest release)")
	cmd.Flags().StringVar(&opts.loaderName, "loader", noLoader, "mod loader: "+noLoader+", "+strings.Join(loader.Names(), ", "))
	cmd.Flags().StringVar(&opts.loaderVersion, "loader-version", "*", "loader version range")
	cmd.Flags().StringVar(&opts.side, "side", "client", "side to declare: client or server")
	opts.aliases = map[string]*aliasFlags{
		"loader": newAliasFlags(cmd, "loader", loader.Names()),
		"side":   newAliasFlags(cmd, "side", manifest.SideNames),
	}
}

// initDir is where the project goes: -C, or else the current directory, with no manifest in it yet.
func (a *app) initDir() (string, error) {
	dir := a.dir
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		return "", out.Errorf("manifest-exists", "%s already exists here", manifest.FileName)
	}
	return dir, nil
}

// settle takes each value from whichever flag spelled it and checks the flags, before anything
// is asked.
func (opts *initOptions) settle(flags *pflag.FlagSet) error {
	var err error
	if opts.side, err = opts.aliases["side"].pick(flags, opts.side); err != nil {
		return err
	}
	if err := project.CheckSide(opts.side, "--side"); err != nil {
		return err
	}
	if opts.loaderName, err = opts.aliases["loader"].pick(flags, opts.loaderName); err != nil {
		return err
	}
	loaders := append([]string{noLoader}, loader.Names()...)
	if _, ok := loader.Lookup(opts.loaderName); !ok && opts.loaderName != noLoader {
		e := out.Errorf("usage", "unknown loader %q", opts.loaderName)
		e.Help = fmt.Sprintf("use one of %s", strings.Join(loaders, ", "))
		e.Candidates, e.Given, e.Flag = loaders, opts.loaderName, "--loader"
		return e
	}
	if opts.loaderName == noLoader && flags.Changed("loader-version") {
		return out.Errorf("usage", "--loader-version needs --loader")
	}
	return nil
}

// given reports whether a flag answered its question, by its own name or an alias of one of
// its values.
func (opts *initOptions) given(flags *pflag.FlagSet, name string) bool {
	return flags.Changed(name) || opts.aliases[name] != nil && opts.aliases[name].chosen != ""
}

// createProject does what init and create share once every choice is made: writes the manifest
// and lock, scaffolds the folder, adds the pack asked for, and prints the result.
func (a *app) createProject(cmd *cobra.Command, dir string, opts *initOptions) error {
	var projectLoader manifest.Loader
	if opts.loaderName != noLoader {
		projectLoader = manifest.Loader{Type: opts.loaderName, Version: opts.loaderVersion}
	}
	m := project.NewManifest(opts.name, dir, opts.minecraft, projectLoader, opts.side)
	d, err := a.deps()
	if err != nil {
		return err
	}
	a.progress("%s", resolve.ResolvingLine(m.Minecraft, projectLoader))
	l, warning, err := d.meta.NewLock(cmd.Context(), m)
	if err != nil {
		return err
	}
	if warning != "" {
		a.printer.Warn("%s", warning)
	}
	if m.Minecraft == "*" {
		m.Minecraft = l.Minecraft
	}
	p := &project.Project{Dir: dir, Manifest: m, Lock: l}
	if err := p.SaveManifest(); err != nil {
		return err
	}
	if err := p.SaveLock(); err != nil {
		os.Remove(filepath.Join(dir, manifest.FileName))
		return err
	}
	if err := project.Scaffold(dir); err != nil {
		return err
	}
	packItems, err := a.initPack(cmd, p, opts.pack)
	if err != nil {
		return err
	}
	res := initResult{Name: m.Name, Minecraft: l.Minecraft, Loader: l.Loader.Type, Version: l.Loader.Version, Java: l.Java.Major, Side: opts.side}
	return a.printer.Emit(res, func(l *out.Lines) {
		l.OK("Created "+manifest.FileName, fmt.Sprintf("%s, Java %d", resolve.PlatformLabel(res.Minecraft, res.Loader, res.Version), res.Java))
		packItems(l)
		switch {
		case res.Loader != "":
			l.Nudge("Add a mod", "shulker add <mod>")
		case opts.side == "server":
			l.Nudge("Download and build it", "shulker install")
		default:
			l.Nudge("Play it in a launcher", "shulker link <launcher>")
		}
	})
}

const noLoader = "none"
