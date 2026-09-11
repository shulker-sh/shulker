package resolve

import (
	"context"
	"encoding/json"
	"fmt"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/loaderver"
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
	Quilt  *meta.Quilt
	Cache  *cache.Cache
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
	if _, err := loader.Require(m.Loader.Type); err != nil {
		return nil, err
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
	provides, err := mt.loaderProvides(ctx, m.Loader.Type, game.ID, loaderVersion)
	if err != nil {
		return nil, err
	}
	return &Platform{
		Minecraft: game.ID,
		Loader:    lock.Loader{Type: m.Loader.Type, Version: loaderVersion, Provides: provides},
		Java:      lock.Java{Major: java.Major, Component: java.Component},
	}, nil
}

type loaderVersions interface {
	LoaderVersions(ctx context.Context, game string) ([]meta.LoaderVersion, error)
}

func (mt *Meta) versions(name string) (loaderVersions, error) {
	switch name {
	case "fabric":
		return mt.Fabric, nil
	case "quilt":
		return mt.Quilt, nil
	}
	return nil, fmt.Errorf("no version list for the %s loader", name)
}

func (mt *Meta) LoaderProfile(ctx context.Context, l lock.Loader, game string) (json.RawMessage, error) {
	switch l.Type {
	case "fabric":
		return mt.Fabric.LoaderProfile(ctx, game, l.Version)
	case "quilt":
		return mt.Quilt.LoaderProfile(ctx, game, l.Version)
	}
	return nil, fmt.Errorf("no launcher profile for the %s loader", l.Type)
}

func (mt *Meta) loaderVersion(ctx context.Context, l manifest.Loader, game string) (string, error) {
	rng, err := loaderver.ParseRange(l.Version)
	if err != nil {
		return "", fmt.Errorf("manifest loader version: %w", err)
	}
	src, err := mt.versions(l.Type)
	if err != nil {
		return "", err
	}
	versions, err := src.LoaderVersions(ctx, game)
	if err != nil {
		return "", err
	}
	var candidates []loaderver.Version
	for _, lv := range versions {
		if v, err := loaderver.Parse(lv.Version); err == nil && (lv.Stable || !rng.IsAny()) {
			candidates = append(candidates, v)
		}
	}
	v, ok := loaderver.Newest(candidates, rng)
	if !ok {
		return "", fmt.Errorf("no %s loader version matches %q for Minecraft %s", l.Type, l.Version, game)
	}
	return v.ID, nil
}

func (mt *Meta) loaderProvides(ctx context.Context, name, game, version string) (map[string]string, error) {
	if name != "quilt" {
		return nil, nil
	}
	url, err := mt.Quilt.LoaderJarURL(ctx, game, version)
	if err != nil {
		return nil, err
	}
	sha, err := mt.Cache.Fetch(ctx, mt.Quilt.Client, url)
	if err != nil {
		return nil, err
	}
	info, err := jarmeta.Read(mt.Cache.Path(sha))
	if err != nil {
		return nil, err
	}
	return info.Provides, nil
}
