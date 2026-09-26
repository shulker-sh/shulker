package game

import (
	"context"
	"path"

	"golang.org/x/sync/errgroup"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
)

// Sources is where the store fills from: Mojang's index for the vanilla version JSON, the fetch
// client for every download, and the loader row the lock names with what it reaches out with.
type Sources struct {
	Fetch  *fetch.Client
	Piston *mojang.Piston
	// Loader is the row the lock names; a lock without a loader leaves it zero.
	Loader  loader.Loader
	Loaders *loader.Remote
	// InstallerJava is the Java the loader's own installer runs with, asked for only when one runs.
	InstallerJava func(ctx context.Context) (string, error)
	// SaveLock saves the lock after a row call locked something new, whether or not the call
	// succeeded, since what it locked stays valid.
	SaveLock func() error
	// Log reports a step of its own, one that is not a download bar.
	Log func(format string, args ...any)
	// Progress starts a bar for one stage's downloads; nil, or a nil bar, reports nothing.
	Progress func(verb string, files []out.Download) *out.Progress
}

func (src Sources) log(format string, args ...any) {
	if src.Log != nil {
		src.Log(format, args...)
	}
}

// Launchable is a version the store holds everything for: the id it runs under, the version JSON
// at the top of its chain, the merged version, and this platform's share of the store.
type Launchable struct {
	ID       string
	Top      Version
	Version  Version
	Assembly Assembly
}

// downloadJobs is how many files the store fetches at once. An asset index names thousands of
// small objects, and fetching them one at a time is what makes a first launch take hours.
const downloadJobs = 8

// Fill makes the locked version launchable on the platform, stage by stage: the vanilla version
// JSON, the loader's version on top of it, then the client jar, the libraries, the asset index and
// the assets it names. Each stage reports itself, so a first launch shows what it is waiting for.
func (s Store) Fill(ctx context.Context, lk *lock.Lock, p Platform, src Sources) (Launchable, error) {
	id, err := s.fillVersion(ctx, lk, src)
	if err != nil {
		return Launchable{}, err
	}
	top, err := s.Version(id)
	if err != nil {
		return Launchable{}, err
	}
	v, err := s.Resolve(id)
	if err != nil {
		return Launchable{}, err
	}
	assembly, err := Assemble(v, p, nil)
	if err != nil {
		return Launchable{}, err
	}
	if err := s.fillAssembly(ctx, src, assembly); err != nil {
		return Launchable{}, err
	}
	return Launchable{ID: id, Top: top, Version: v, Assembly: assembly}, nil
}

// fillVersion puts the version JSON a launch runs, and the vanilla one it inherits from, into the
// store, and returns its id. A loader with an installer of its own is run against the store, which
// is laid out as the Mojang launcher directory the installer expects. The store remembers the id
// each loader wrote, so a later launch needs neither the installer nor the network.
func (s Store) fillVersion(ctx context.Context, lk *lock.Lock, src Sources) (string, error) {
	if err := s.fillVanillaVersion(ctx, src, lk.Minecraft); err != nil {
		return "", err
	}
	if lk.Loader.Type == "" {
		return lk.Minecraft, nil
	}
	key := lk.Loader.Type + "-" + lk.Loader.Version + "-" + lk.Minecraft
	if id, ok := s.InstalledLoader(key); ok && s.HasVersion(id) {
		return id, nil
	}
	var id string
	var err error
	if src.Loader.HasInstaller() {
		if err := s.fillVanillaClient(ctx, src, lk.Minecraft); err != nil {
			return "", err
		}
		id, err = InstallLoader(ctx, s.Root, lk, src)
	} else {
		src.log("fetching %s loader %s for %s", loader.Title(lk.Loader.Type), lk.Loader.Version, lk.Minecraft)
		var profile []byte
		if profile, err = src.Loader.Profile(ctx, src.Loaders, lk.Minecraft, lk.Loader.Version); err == nil {
			id, err = s.SaveVersion(profile)
		}
	}
	if err != nil {
		return "", err
	}
	return id, s.RecordLoader(key, id)
}

func (s Store) fillVanillaVersion(ctx context.Context, src Sources, minecraft string) error {
	if s.HasVersion(minecraft) {
		return nil
	}
	src.log("fetching the Minecraft %s version json", minecraft)
	raw, err := src.Piston.Version(ctx, minecraft)
	if err != nil {
		return err
	}
	_, err = s.SaveVersion(raw)
	return err
}

// fillVanillaClient puts the vanilla client jar in the store before a loader's installer runs.
// The installer patches that jar and would download it itself when it is missing, but silently,
// with no progress line of its own.
func (s Store) fillVanillaClient(ctx context.Context, src Sources, minecraft string) error {
	v, err := s.Version(minecraft)
	if err != nil {
		return err
	}
	client, err := ClientJar(v)
	if err != nil {
		return err
	}
	return s.fetchInto(ctx, src, []File{client}, "client jar", "client jars")
}

// InstallLoader runs the loader's own installer into dir, laid out the way the Mojang launcher
// directory the installer expects is, and returns the version id it wrote: the store for a launch,
// the launcher's own directory for a link into it. The row locks the installer jar the first time,
// so the lock is saved whether or not the run succeeds; the run's own error wins over a save failure.
func InstallLoader(ctx context.Context, dir string, lk *lock.Lock, src Sources) (string, error) {
	java, err := src.InstallerJava(ctx)
	if err != nil {
		return "", err
	}
	return (&launcher.Mojang{Dir: dir}).InstallLoader(src.Loader.Name, func() error {
		changed, err := src.Loader.InstallClient(ctx, src.Loaders, lk, dir, java)
		if !changed || src.SaveLock == nil {
			return err
		}
		if saveErr := src.SaveLock(); saveErr != nil && err == nil {
			return saveErr
		}
		return err
	})
}

// fillAssembly fetches everything the assembly is missing, a bar per kind so each stage of the
// assembly says what it did. Assets come last, because their index has to be in the store before
// the objects it names can be listed.
func (s Store) fillAssembly(ctx context.Context, src Sources, assembly Assembly) error {
	if err := s.fetchInto(ctx, src, []File{assembly.Client}, "client jar", "client jars"); err != nil {
		return err
	}
	libraries := append(append([]File{}, assembly.Libraries...), assembly.Natives...)
	if err := s.fetchInto(ctx, src, libraries, "library", "libraries"); err != nil {
		return err
	}
	if assembly.AssetIndex.Path == "" {
		return nil
	}
	if err := s.fetchInto(ctx, src, []File{assembly.AssetIndex}, "asset index", "asset indexes"); err != nil {
		return err
	}
	objects, err := s.AssetFiles(assembly.Version.AssetIndex.ID)
	if err != nil {
		return err
	}
	return s.fetchInto(ctx, src, objects, "asset", "assets")
}

func (s Store) fetchInto(ctx context.Context, src Sources, files []File, one, many string) error {
	var missing []File
	for _, f := range files {
		if !s.Has(f) {
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	downloads := make([]out.Download, len(missing))
	for i, f := range missing {
		downloads[i] = out.Download{Name: path.Base(f.Path), Size: f.Size}
	}
	var progress *out.Progress
	if src.Progress != nil {
		progress = src.Progress("fetching", downloads).Counts(one, many)
	}
	if progress != nil {
		src.Fetch.Progress = progress.Bytes
		defer func() { src.Fetch.Progress = nil }()
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(downloadJobs)
	for i, f := range missing {
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			progress.File(downloads[i].Name)
			if err := s.Fetch(ctx, src.Fetch, f); err != nil {
				return err
			}
			progress.Advance()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		progress.Abort()
		return err
	}
	progress.Finish()
	return nil
}
