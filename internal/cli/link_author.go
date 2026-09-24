package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/pack"
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
			return a.openSource(ctx, source, pack.At{})
		}
	}
	opts := initOptions{loaderName: noLoader, loaderVersion: "*"}
	if err := a.askPlatform(ctx, &opts, func(string) bool { return false }); err != nil {
		return nil, err
	}
	m := &manifest.Manifest{
		Schema:    manifest.SchemaURL,
		Minecraft: project.OrLatest(opts.minecraft),
		Requires:  map[string]manifest.Require{},
		Client:    project.NewClient(),
	}
	if opts.loaderName != noLoader {
		m.Loader = manifest.Loader{Type: opts.loaderName, Version: opts.loaderVersion}
	}
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	a.progress("%s", resolve.ResolvingLine(m.Minecraft, m.Loader))
	platform, err := d.meta.Platform(ctx, m, nil)
	if err != nil {
		return nil, err
	}
	if m.Minecraft == "*" {
		m.Minecraft = platform.Minecraft
	}
	l := lock.New()
	l.Minecraft, l.Loader, l.Java = platform.Minecraft, platform.Loader, platform.Java
	display := project.PlatformName(platform.Minecraft, platform.Loader.Type)
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("as") {
		if display, err = a.askText("What should it be called?", "the name the launcher shows", display); err != nil {
			return nil, err
		}
	}
	m.Name, m.Client.Name = config.SlugID(display), display
	return &syncSource{Checkout: &pack.Checkout{Kind: pack.Local}, project: &project.Project{Manifest: m, Lock: l}, isAuthor: true}, nil
}
