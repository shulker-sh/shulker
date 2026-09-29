package cli

import (
	"context"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) initCmd() *cobra.Command {
	var opts initOptions
	cmd := &cobra.Command{
		Use:         "init",
		Annotations: acts(),
		Short:       "Create shulker.json and a lock in the current directory, asking what is not given",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := a.initDir()
			if err != nil {
				return err
			}
			if err := opts.settle(cmd.Flags()); err != nil {
				return err
			}
			if err := a.askInit(cmd, &opts); err != nil {
				return err
			}
			return a.createProject(cmd, dir, &opts)
		},
	}
	a.uncheckedDirFlag(cmd)
	initFlags(cmd, &opts)
	return cmd
}

// askInit walks the wizard's questions in order. Each is skipped by the flag that answers it,
// and every default is that flag's, so accepting them all creates what create creates.
func (a *app) askInit(cmd *cobra.Command, opts *initOptions) error {
	if !a.canPick() {
		return nil
	}
	given := func(flag string) bool { return opts.given(cmd.Flags(), flag) }
	if !given("side") {
		side, err := a.ask("What are you making?", []out.Choice{
			{Label: "a client pack", Value: "client"},
			{Label: "a server pack", Value: "server"},
		})
		if err != nil {
			return err
		}
		opts.side = side
	}
	if err := a.askPlatform(cmd.Context(), opts, given); err != nil {
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
