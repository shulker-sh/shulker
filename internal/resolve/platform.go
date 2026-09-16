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
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

type Platform struct {
	Minecraft string
	Loader    lock.Loader
	Java      lock.Java
}

type Meta struct {
	Piston   *meta.Piston
	Fabric   *meta.Fabric
	Quilt    *meta.Quilt
	NeoForge *meta.NeoForge
	Forge    *meta.Forge
	Cache    *cache.Cache
}

// Platform is nil, with no error, when the manifest sets no Minecraft version and no
// locked modpack supplies one. The relock leaves the lock's platform alone then, and
// Validate refuses it at the end, once the command body has had its chance to add the
// modpack that supplies one.
func (mt *Meta) Platform(ctx context.Context, m *manifest.Manifest, packs []*pack.Loaded) (*Platform, error) {
	inherited, err := inheritedPlatform(m, packs)
	if err != nil {
		return nil, err
	}
	minecraft := m.Minecraft
	if minecraft == "" {
		if inherited.Minecraft == "" {
			return nil, nil
		}
		minecraft = inherited.Minecraft
	}
	games, err := mt.Piston.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	rng, err := mcver.ParseRange(minecraft)
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
		return nil, fmt.Errorf("no Minecraft version matches %q (latest release is %s)", minecraft, games.Latest.Release)
	}
	if m.Loader.Type != "" {
		if _, err := loader.Require(m.Loader.Type); err != nil {
			return nil, err
		}
	}
	entry, _ := games.Find(game.ID)
	java, err := mt.Piston.Java(ctx, entry)
	if err != nil {
		return nil, err
	}
	platform := &Platform{Minecraft: game.ID, Java: lock.Java{Major: java.Major, Component: java.Component}}
	if m.Loader.Type == "" {
		platform.Loader = inherited.Loader
		return platform, nil
	}
	loaderVersion, err := mt.loaderVersion(ctx, m.Loader, game.ID)
	if err != nil {
		return nil, err
	}
	provides, err := mt.loaderProvides(ctx, m.Loader.Type, game.ID, loaderVersion)
	if err != nil {
		return nil, err
	}
	platform.Loader = lock.Loader{Type: m.Loader.Type, Version: loaderVersion, Provides: provides}
	return platform, nil
}

// inheritedDifferences reports a platform the locked modpacks supply that the lock
// doesn't hold yet, so a modpack moving to another Minecraft or loader re-resolves
// the projects that inherit from it instead of leaving them on a stale lock.
func (r *Resolver) inheritedDifferences() ([]string, error) {
	inherited, err := inheritedPlatform(r.Manifest, r.Packs)
	if err != nil {
		return nil, err
	}
	var diffs []string
	if inherited.Minecraft != "" && inherited.Minecraft != r.Lock.Minecraft {
		diffs = append(diffs, fmt.Sprintf("minecraft: locked %s, the modpacks pin %s", r.Lock.Minecraft, inherited.Minecraft))
	}
	if inherited.Loader.Type != "" && (inherited.Loader.Type != r.Lock.Loader.Type || inherited.Loader.Version != r.Lock.Loader.Version) {
		diffs = append(diffs, fmt.Sprintf("loader: locked %s, the modpacks pin %s", loader.Describe(r.Lock.Loader.Type, r.Lock.Loader.Version), loader.Describe(inherited.Loader.Type, inherited.Loader.Version)))
	}
	return diffs, nil
}

// inheritedPlatform takes the Minecraft version and loader the manifest leaves out
// from the locked modpacks. Each pins exact versions, so they all have to agree.
func inheritedPlatform(m *manifest.Manifest, packs []*pack.Loaded) (*Platform, error) {
	p := &Platform{}
	var minecraftFrom, loaderFrom string
	for _, l := range packs {
		if !l.Locked || l.Lock == nil {
			continue
		}
		if m.Minecraft == "" {
			if minecraftFrom != "" && l.Lock.Minecraft != p.Minecraft {
				return nil, out.Errorf("modpack-platform", "locked modpacks %s and %s are built for minecraft %s and %s, and this project sets none; set minecraft in shulker.json, or unlock one", minecraftFrom, l.Name, p.Minecraft, l.Lock.Minecraft)
			}
			p.Minecraft, minecraftFrom = l.Lock.Minecraft, l.Name
		}
		if m.Loader.Type == "" {
			if loaderFrom != "" && (l.Lock.Loader.Type != p.Loader.Type || l.Lock.Loader.Version != p.Loader.Version) {
				return nil, out.Errorf("modpack-platform", "locked modpacks %s and %s are built for %s and %s, and this project sets no loader; set loader in shulker.json, or unlock one", loaderFrom, l.Name, loader.Describe(p.Loader.Type, p.Loader.Version), loader.Describe(l.Lock.Loader.Type, l.Lock.Loader.Version))
			}
			p.Loader, loaderFrom = l.Lock.Loader, l.Name
		}
	}
	return p, nil
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
	case "neoforge":
		return mt.NeoForge, nil
	case "forge":
		return mt.Forge, nil
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

func (mt *Meta) InstallerURL(lk *lock.Lock) (string, error) {
	switch lk.Loader.Type {
	case "neoforge":
		return mt.NeoForge.InstallerURL(lk.Loader.Version), nil
	case "forge":
		return mt.Forge.InstallerURL(lk.Minecraft, lk.Loader.Version), nil
	}
	return "", fmt.Errorf("no installer for the %s loader", lk.Loader.Type)
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
	info, err := jarmeta.Read(mt.Cache.Object(sha), name)
	if err != nil {
		return nil, err
	}
	return info.Provides, nil
}
