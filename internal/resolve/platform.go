package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"

	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/loaderver"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

// Platform is the Minecraft version, loader and Java a project resolves to.
type Platform struct {
	Minecraft string
	Loader    lock.Loader
	Java      lock.Java
}

// Meta reads the version metadata a platform is resolved from: Mojang's index and each loader's own.
type Meta struct {
	Piston  *mojang.Piston
	Loaders *loader.Remote
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
	game, err := newestGame(games, minecraft)
	if err != nil {
		return nil, err
	}
	var row loader.Loader
	if m.Loader.Type != "" {
		if row, err = loader.Require(m.Loader.Type); err != nil {
			return nil, err
		}
	}
	entry, _ := games.Find(game)
	java, err := mt.Piston.Java(ctx, entry)
	if err != nil {
		return nil, err
	}
	platform := &Platform{Minecraft: game, Java: lock.Java{Major: java.Major, Component: java.Component}}
	if m.Loader.Type == "" {
		platform.Loader = inherited.Loader
		// A modpack archive names its loader's version but not what that loader provides.
		if l := platform.Loader; l.Type != "" {
			if err := loader.Supports(l.Type, game, l.Version); err != nil {
				return nil, err
			}
		}
		if l := platform.Loader; l.Type != "" && l.Provides == nil {
			inheritedRow, err := loader.Require(l.Type)
			if err != nil {
				return nil, err
			}
			if platform.Loader.Provides, err = mt.loaderProvides(ctx, inheritedRow, game, l.Version); err != nil {
				return nil, err
			}
		}
		return platform, nil
	}
	loaderVersion, err := mt.loaderVersion(ctx, row, m.Loader.Version, game)
	if err != nil {
		return nil, err
	}
	if err := loader.Supports(m.Loader.Type, game, loaderVersion); err != nil {
		return nil, err
	}
	provides, err := mt.loaderProvides(ctx, row, game, loaderVersion)
	if err != nil {
		return nil, err
	}
	platform.Loader = lock.Loader{Type: m.Loader.Type, Version: loaderVersion, Provides: provides}
	return platform, nil
}

// firstWithDataVersion is the first 1.14 snapshot, where the server jar's version.json starts.
var firstWithDataVersion = mcver.MustParse("1.13").TildeUpper()

// FillDataVersion gives l the data version of its Minecraft version when it has none yet, and none
// is looked for before 1.14. Reading it never blocks a lock: a failure comes back as a warning, l
// stays without one, and the next relock tries again.
func (mt *Meta) FillDataVersion(ctx context.Context, l *lock.Lock) (warning string) {
	if l.Minecraft == "" || l.DataVersion != 0 {
		return ""
	}
	if v, err := mcver.Parse(l.Minecraft); err == nil && v.Compare(firstWithDataVersion) < 0 {
		return ""
	}
	dataVersion, err := mt.Piston.DataVersion(ctx, l.Minecraft)
	if err != nil {
		return fmt.Sprintf("couldn't read the Minecraft %s data version, so ${minecraft.dataVersion} stays unset until the next relock: %v", l.Minecraft, err)
	}
	l.DataVersion = dataVersion
	return ""
}

// NewLock starts a new project's lock from the exact platform m names: Minecraft, loader, Java
// and the data version, whose failure to read comes back as the warning FillDataVersion gives.
func (mt *Meta) NewLock(ctx context.Context, m *manifest.Manifest) (*lock.Lock, string, error) {
	platform, err := mt.Platform(ctx, m, nil)
	if err != nil {
		return nil, "", err
	}
	l := lock.New()
	l.Minecraft = platform.Minecraft
	l.Loader = platform.Loader
	l.Java = platform.Java
	return l, mt.FillDataVersion(ctx, l), nil
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
		if !l.UsesLock || l.Lock == nil {
			continue
		}
		if m.Minecraft == "" {
			if minecraftFrom != "" && l.Lock.Minecraft != p.Minecraft {
				e := out.Errorf("modpack-platform", "locked modpacks %s and %s are built for minecraft %s and %s, and this project sets none", minecraftFrom, l.Name, p.Minecraft, l.Lock.Minecraft)
				e.Help = "set minecraft in shulker.json, or unlock one"
				return nil, e
			}
			p.Minecraft, minecraftFrom = l.Lock.Minecraft, l.Name
		}
		if m.Loader.Type == "" {
			if loaderFrom != "" && (l.Lock.Loader.Type != p.Loader.Type || l.Lock.Loader.Version != p.Loader.Version) {
				e := out.Errorf("modpack-platform", "locked modpacks %s and %s are built for %s and %s, and this project sets no loader", loaderFrom, l.Name, loader.Describe(p.Loader.Type, p.Loader.Version), loader.Describe(l.Lock.Loader.Type, l.Lock.Loader.Version))
				e.Help = "set loader in shulker.json, or unlock one"
				return nil, e
			}
			p.Loader, loaderFrom = l.Lock.Loader, l.Name
		}
	}
	return p, nil
}

// LoaderProfile is the launcher profile JSON a loader's meta serves for the locked loader.
func (mt *Meta) LoaderProfile(ctx context.Context, l lock.Loader, game string) (json.RawMessage, error) {
	row, err := loader.Require(l.Type)
	if err != nil {
		return nil, err
	}
	return row.Profile(ctx, mt.Loaders, game, l.Version)
}

func (mt *Meta) loaderVersion(ctx context.Context, row loader.Loader, rng, game string) (string, error) {
	r, err := loaderver.ParseRange(rng)
	if err != nil {
		return "", rangeInvalid("loader.version", rng, err)
	}
	versions, err := row.Versions(ctx, mt.Loaders, game)
	if err != nil {
		return "", err
	}
	var candidates []loaderver.Version
	for _, lv := range versions {
		if v, err := loaderver.Parse(lv.Version); err == nil && (lv.Stable || !r.IsAny()) {
			candidates = append(candidates, v)
		}
	}
	v, ok := loaderver.Newest(candidates, r)
	if !ok {
		return "", out.Errorf("platform-not-found", "no %s loader version matches %q for minecraft %s", row.Name, rng, game)
	}
	return v.ID, nil
}

// loaderProvides is what the loader's own jar says it provides in place of another loader, for a
// loader whose jar declares that; the jar is cached and hashed here, since its meta's hashes can't
// be trusted.
func (mt *Meta) loaderProvides(ctx context.Context, row loader.Loader, game, version string) (map[string]string, error) {
	url, ok, err := row.ProvidesJar(ctx, mt.Loaders, game, version)
	if err != nil || !ok {
		return nil, err
	}
	sha, err := mt.Loaders.Cache.Fetch(ctx, mt.Loaders.Fetch, url)
	if err != nil {
		return nil, err
	}
	info, err := jarmeta.Read(mt.Loaders.Cache.Object(sha), path.Base(url), row)
	if err != nil {
		return nil, prefixed(row.Name+" loader "+version, err)
	}
	return info.AllProvides(), nil
}

// GameVersion is the Minecraft version a range resolves to, for a caller that needs it before
// the rest of the platform.
func (mt *Meta) GameVersion(ctx context.Context, minecraft string) (string, error) {
	games, err := mt.Piston.Manifest(ctx)
	if err != nil {
		return "", err
	}
	return newestGame(games, minecraft)
}

// newestGame is the newest version the manifest lists that matches minecraft. An any range takes
// the newest release, so a project that names no version is never authored against a snapshot.
func newestGame(games *mojang.GameManifest, minecraft string) (string, error) {
	rng, err := mcver.ParseRange(minecraft)
	if err != nil {
		return "", rangeInvalid("minecraft", minecraft, err)
	}
	var candidates []mcver.Version
	for _, g := range games.Versions {
		if v, err := mcver.Parse(g.ID); err == nil {
			candidates = append(candidates, v)
		}
	}
	game, ok := mcver.Newest(candidates, rng)
	if !ok {
		e := out.Errorf("platform-not-found", "no minecraft version matches %q", minecraft)
		e.Rows = []out.Detail{{Label: "latest release", Text: games.Latest.Release}}
		return "", e
	}
	return game.ID, nil
}

// GameVersions lists the Minecraft releases newest first, and the latest release, for a prompt
// that offers them. Snapshots are left out: a project is authored against a release.
func (mt *Meta) GameVersions(ctx context.Context) ([]string, string, error) {
	games, err := mt.Piston.Manifest(ctx)
	if err != nil {
		return nil, "", err
	}
	var releases []mcver.Version
	for _, g := range games.Versions {
		if v, err := mcver.Parse(g.ID); err == nil && v.IsRelease() {
			releases = append(releases, v)
		}
	}
	slices.SortFunc(releases, func(a, b mcver.Version) int { return mcver.Compare(b, a) })
	ids := make([]string, len(releases))
	for i, v := range releases {
		ids[i] = v.ID
	}
	return ids, games.Latest.Release, nil
}

// LoaderVersions lists a loader's versions for one Minecraft version, newest first, and the one
// "*" resolves to, which is the newest stable.
func (mt *Meta) LoaderVersions(ctx context.Context, name, game string) ([]string, string, error) {
	row, err := loader.Require(name)
	if err != nil {
		return nil, "", err
	}
	list, err := row.Versions(ctx, mt.Loaders, game)
	if err != nil {
		return nil, "", err
	}
	var all, stable []loaderver.Version
	for _, lv := range list {
		v, err := loaderver.Parse(lv.Version)
		if err != nil {
			continue
		}
		all = append(all, v)
		if lv.Stable {
			stable = append(stable, v)
		}
	}
	slices.SortFunc(all, func(a, b loaderver.Version) int { return loaderver.Compare(b, a) })
	ids := make([]string, len(all))
	for i, v := range all {
		ids[i] = v.ID
	}
	newest, _ := loaderver.Newest(stable, loaderver.Range{})
	return ids, newest.ID, nil
}
