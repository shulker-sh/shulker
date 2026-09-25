package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// authorSource asks init's questions for a link with nothing to follow. A pack comes first, since a
// locked pack decides the platform the rest would ask about, and is followed as if it had been
// named. Otherwise the answers become a project with no directory yet, which the link writes into
// the instance once it knows where that is.
func (a *app) authorSource(cmd *cobra.Command) (*syncSource, error) {
	ctx := cmd.Context()
	from, err := a.ask("Start from an existing pack?", packChoices)
	if err != nil {
		return nil, err
	}
	if from == "pack" {
		source, err := a.askPack()
		if err != nil {
			return nil, err
		}
		if source != "" {
			return a.openSource(ctx, source, modpack.At{})
		}
	}
	opts := initOptions{loaderName: noLoader, loaderVersion: "*"}
	if err := a.askPlatform(ctx, &opts, func(string) bool { return false }); err != nil {
		return nil, err
	}
	var l manifest.Loader
	if opts.loaderName != noLoader {
		l = manifest.Loader{Type: opts.loaderName, Version: opts.loaderVersion}
	}
	minecraft := project.OrLatest(opts.minecraft)
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	a.progress("%s", resolve.ResolvingLine(minecraft, l))
	platform, err := d.meta.Platform(ctx, &manifest.Manifest{Minecraft: minecraft, Loader: l}, nil)
	if err != nil {
		return nil, err
	}
	locked := lock.New()
	locked.Minecraft, locked.Loader, locked.Java = platform.Minecraft, platform.Loader, platform.Java
	display := project.PlatformName(platform.Minecraft, platform.Loader.Type)
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("as") {
		if display, err = a.askText("What should it be called?", "the name the launcher shows", display); err != nil {
			return nil, err
		}
	}
	return &syncSource{Checkout: &modpack.Checkout{Kind: modpack.Local}, project: project.Authored(locked, minecraft, l, display), isAuthor: true}, nil
}
