package resolve

import (
	"context"
	"fmt"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/meta"
)

type Platform struct {
	Minecraft string
	Loader    lock.Loader
	Java      lock.Java
}

type Meta struct {
	Piston *meta.Piston
	Fabric *meta.Fabric
}

func (mt *Meta) Platform(ctx context.Context, m *manifest.Manifest) (*Platform, error) {
	games, err := mt.Piston.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	rng, err := mcver.ParseRange(m.Minecraft)
	if err != nil {
		return nil, fmt.Errorf("manifest minecraft: %w", err)
	}
	var candidates []mcver.Version
	for _, g := range games.Versions {
		if v, err := mcver.Parse(g.ID); err == nil {
			candidates = append(candidates, v)
		}
	}
	game, ok := mcver.Newest(candidates, rng)
	if !ok {
		return nil, fmt.Errorf("no Minecraft version matches %q (latest release is %s)", m.Minecraft, games.Latest.Release)
	}
	entry, _ := games.Find(game.ID)
	java, err := mt.Piston.Java(ctx, entry)
	if err != nil {
		return nil, err
	}
	loaderVersion, err := mt.loaderVersion(ctx, m.Loader, game.ID)
	if err != nil {
		return nil, err
	}
	return &Platform{
		Minecraft: game.ID,
		Loader:    lock.Loader{Type: m.Loader.Type, Version: loaderVersion},
		Java:      lock.Java{Major: java.Major, Component: java.Component},
	}, nil
}

func (mt *Meta) loaderVersion(ctx context.Context, l manifest.Loader, game string) (string, error) {
	if _, err := loader.Require(l.Type); err != nil {
		return "", err
	}
	rng, err := mcver.ParseRange(l.Version)
	if err != nil {
		return "", fmt.Errorf("manifest loader version: %w", err)
	}
	versions, err := mt.Fabric.LoaderVersions(ctx, game)
	if err != nil {
		return "", err
	}
	var candidates []mcver.Version
	for _, lv := range versions {
		if v, err := mcver.Parse(lv.Version); err == nil && (lv.Stable || !rng.IsAny()) {
			candidates = append(candidates, v)
		}
	}
	v, ok := mcver.Newest(candidates, rng)
	if !ok {
		return "", fmt.Errorf("no fabric loader version matches %q for Minecraft %s", l.Version, game)
	}
	return v.ID, nil
}
