package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
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

// initOptions is what the flags set and what the wizard fills in for the ones left out.
type initOptions struct {
	yes           bool
	name          string
	minecraft     string
	loaderName    string
	loaderVersion string
	side          string
	pack          string
}

func (a *app) initCmd() *cobra.Command {
	var opts initOptions
	cmd := &cobra.Command{
		Use:         "init",
		Annotations: acts(),
		Short:       "Create shulker.json and a lock in the current directory",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := a.dir
			if dir == "" {
				var err error
				if dir, err = os.Getwd(); err != nil {
					return err
				}
			}
			if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
				return out.Errorf("manifest-exists", "%s already exists here", manifest.FileName)
			}
			if err := checkSide(opts.side, "--side"); err != nil {
				return err
			}
			loaders := append([]string{noLoader}, loader.Names()...)
			if _, ok := loader.Lookup(opts.loaderName); !ok && opts.loaderName != noLoader {
				e := out.Errorf("usage", "unknown loader %q", opts.loaderName)
				e.Help = fmt.Sprintf("use one of %s", strings.Join(loaders, ", "))
				e.Candidates, e.Given, e.Flag = loaders, opts.loaderName, "--loader"
				return e
			}
			if opts.loaderName == noLoader && cmd.Flags().Changed("loader-version") {
				return out.Errorf("usage", "--loader-version needs --loader")
			}
			// --yes is init's own spelling of --no-input, since the wizard leaves the
			// command no required value.
			if opts.yes {
				a.printer.NoInput = true
			}
			if err := a.askInit(cmd, &opts); err != nil {
				return err
			}
			var projectLoader manifest.Loader
			if opts.loaderName != noLoader {
				projectLoader = manifest.Loader{Type: opts.loaderName, Version: opts.loaderVersion}
			}
			name := opts.name
			if name == "" {
				name = project.Slugify(filepath.Base(dir))
			}
			minecraft := project.OrLatest(opts.minecraft)
			m := &manifest.Manifest{
				Schema:    manifest.SchemaURL,
				Name:      name,
				Authors:   defaultAuthors(),
				Minecraft: minecraft,
				Loader:    projectLoader,
				Requires:  map[string]manifest.Require{},
			}
			if opts.side == "server" {
				m.Server = &manifest.Server{EULA: false, Memory: manifest.DefaultServerMemory, Properties: map[string]any{"difficulty": "easy"}}
			}
			if opts.side == "client" {
				m.Client = project.NewClient()
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			a.progress("%s", resolve.ResolvingLine(minecraft, projectLoader))
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
			res := initResult{Name: name, Minecraft: l.Minecraft, Loader: l.Loader.Type, Version: l.Loader.Version, Java: l.Java.Major, Side: opts.side}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.OK("created "+manifest.FileName, fmt.Sprintf("%s, Java %d", resolve.PlatformLabel(res.Minecraft, res.Loader, res.Version), res.Java))
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
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "accept defaults without asking: latest release, no loader, client side")
	cmd.Flags().StringVar(&opts.name, "name", "", "project name (default: directory name)")
	cmd.Flags().StringVar(&opts.minecraft, "minecraft", "", "Minecraft version or range (default: latest release)")
	cmd.Flags().StringVar(&opts.loaderName, "loader", noLoader, "mod loader: "+noLoader+", "+strings.Join(loader.Names(), ", "))
	cmd.Flags().StringVar(&opts.loaderVersion, "loader-version", "*", "loader version range")
	cmd.Flags().StringVar(&opts.side, "side", "client", "side to declare: client or server")
	return cmd
}

// askInit walks the wizard's questions in order. Each is skipped by the flag that answers it,
// and every default is that flag's, so accepting them all creates what --yes creates.
func (a *app) askInit(cmd *cobra.Command, opts *initOptions) error {
	if !a.canPick() {
		return nil
	}
	if !cmd.Flags().Changed("side") {
		side, err := a.ask("What are you making?", []out.Choice{
			{Label: "a client pack", Value: "client"},
			{Label: "a server pack", Value: "server"},
		})
		if err != nil {
			return err
		}
		opts.side = side
	}
	if err := a.askPlatform(cmd.Context(), opts, cmd.Flags().Changed); err != nil {
		return err
	}
	from, err := a.ask("Start from an existing pack?", packChoices)
	if err != nil {
		return err
	}
	if from == "pack" {
		if opts.pack, err = a.askPack(); err != nil {
			return err
		}
	}
	return nil
}

var packChoices = []out.Choice{
	{Label: "start from scratch", Value: ""},
	{Label: "from an existing pack", Value: "pack"},
}

func (a *app) askPack() (string, error) {
	return a.askText("Which pack?", "a local path, git URL, or manifest URL", "")
}

// askPlatform asks the wizard's Minecraft and loader questions, skipping each one whose flag was
// given.
func (a *app) askPlatform(ctx context.Context, opts *initOptions, given func(flag string) bool) error {
	d, err := a.deps()
	if err != nil {
		return err
	}
	t := a.printer.ErrTheme
	// game is what the Minecraft answer resolves to, which the loader's version list needs.
	var game string
	if !given("minecraft") {
		a.progress("fetching Minecraft versions")
		releases, latest, err := d.meta.GameVersions(ctx)
		if err != nil {
			return err
		}
		choices := []out.Choice{latestChoice(t, latest)}
		for _, id := range releases {
			choices = append(choices, out.Choice{Label: id, Value: id})
		}
		if opts.minecraft, err = a.ask("Which Minecraft version?", choices); err != nil {
			return err
		}
		game = opts.minecraft
		if game == "*" {
			game = latest
		}
	}
	if !given("loader") {
		mods, err := a.ask("Add mods?", []out.Choice{{Label: "no", Value: noLoader}, {Label: "yes", Value: "yes"}})
		if err != nil {
			return err
		}
		if mods == "yes" {
			if opts.loaderName, err = a.ask("Which mod loader?", loaderChoices()); err != nil {
				return err
			}
		}
	}
	if opts.loaderName != noLoader && !given("loader-version") {
		a.progress("fetching %s versions", opts.loaderName)
		if game == "" {
			if game, err = d.meta.GameVersion(ctx, project.OrLatest(opts.minecraft)); err != nil {
				return err
			}
		}
		versions, latest, err := d.meta.LoaderVersions(ctx, opts.loaderName, game)
		if err != nil {
			return err
		}
		choices := []out.Choice{latestChoice(t, latest)}
		for _, id := range versions {
			choices = append(choices, out.Choice{Label: id, Value: id})
		}
		if opts.loaderVersion, err = a.ask("Which "+opts.loaderName+" version?", choices); err != nil {
			return err
		}
	}
	return nil
}

func loaderChoices() []out.Choice {
	choices := make([]out.Choice, 0, len(loader.Names()))
	for _, name := range loader.Names() {
		choices = append(choices, out.Choice{Label: name, Value: name})
	}
	return choices
}

// initPack adds the pack the wizard asked for to the project init has just written, and returns
// what it changed, so the lines print under init's own rather than as a second result.
func (a *app) initPack(cmd *cobra.Command, p *project.Project, source string) (func(*out.Lines), error) {
	if source == "" {
		return func(*out.Lines) {}, nil
	}
	rl, err := a.relockOpened(cmd, p, relockOptions{}, func(p *project.Project, r *resolve.Resolver) (string, error) {
		store, err := a.packStore(p)
		if err != nil {
			return "", err
		}
		return "", r.AddPackSource(cmd.Context(), store, source, "", manifest.Require{Source: source})
	})
	if err != nil {
		e := out.AsError(err)
		if e.Nudge.Command == "" {
			e.Nudge = out.Nudge{Lead: "Add the pack to the project just created", Command: "shulker add " + source + " --type modpack"}
		}
		return nil, e
	}
	return rl.printItems, nil
}

const noLoader = "none"

func defaultAuthors() []string {
	authors := []string{"shulker.sh"}
	name, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return authors
	}
	if user := strings.TrimSpace(string(name)); user != "" {
		authors = append([]string{user}, authors...)
	}
	return authors
}
