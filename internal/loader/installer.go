package loader

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"runtime"
	"sort"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

// InstallServer runs the loader's own installer into a built server dir. The build already placed
// every file the installer would download, so it runs offline and only generates the rest.
func (l Loader) InstallServer(ctx context.Context, r *Remote, lk *lock.Lock, dir, java string) error {
	if l.InstallServerFlag == "" {
		return nil
	}
	r.log("installing %s %s", l.Name, lk.Loader.Version)
	return r.RunInstaller(ctx, java, r.Cache.Object(lk.Loader.Server.Sha512), []string{l.InstallServerFlag, dir, "--offline"})
}

// InstallClient runs the loader's own installer into a launcher dir, online, since the client
// libraries it fetches are unlocked. The installer jar itself is locked as loader.client the first
// time, so later runs verify it and hit the cache; changedLock says the caller has a lock to save.
func (l Loader) InstallClient(ctx context.Context, r *Remote, lk *lock.Lock, launcherDir, java string) (changedLock bool, err error) {
	jar, changedLock, err := l.clientInstaller(ctx, r, lk)
	if err != nil {
		return changedLock, err
	}
	r.log("installing %s %s", l.Name, lk.Loader.Version)
	return changedLock, r.RunInstaller(ctx, java, jar, []string{l.InstallClientFlag, launcherDir})
}

// InstallerVersion is the version.json the loader's installer writes into a launcher, read from
// the locked installer jar.
func (l Loader) InstallerVersion(ctx context.Context, r *Remote, lk *lock.Lock) (raw json.RawMessage, changedLock bool, err error) {
	jar, changedLock, err := l.clientInstaller(ctx, r, lk)
	if err != nil {
		return nil, changedLock, err
	}
	raw, err = installerVersionJSON(jar)
	return raw, changedLock, err
}

// clientInstaller is the path to the locked client installer jar, locking it as loader.client the
// first time. A server lock of the same jar is reused rather than downloaded again.
func (l Loader) clientInstaller(ctx context.Context, r *Remote, lk *lock.Lock) (path string, changedLock bool, err error) {
	if l.installerURL == nil {
		return "", false, out.Errorf("unsupported-loader", "shulker has no installer for the %s loader", l.Name)
	}
	locked := &lk.Loader
	if c := locked.Client; c != nil {
		path, err := r.Cache.Ensure(ctx, r.Fetch, c.URL, c.Sha512)
		return path, false, err
	}
	url := l.installerURL(r, lk.Minecraft, locked.Version)
	if s := locked.Server; s != nil && s.URL == url && r.Cache.Has(s.Sha512) {
		locked.Client = &lock.Download{URL: url, Sha512: s.Sha512}
		return r.Cache.Object(s.Sha512), true, nil
	}
	r.log("downloading the %s installer %s", l.Name, locked.Version)
	sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
	if err != nil {
		return "", false, err
	}
	locked.Client = &lock.Download{URL: url, Sha512: sha}
	return r.Cache.Object(sha), true, nil
}

// installerEnsureServer locks the loader's own installer jar plus everything it would download:
// the libraries its profiles list. The build places those and the vanilla server jar, so the
// installer runs offline and only builds what it generates.
func installerEnsureServer(ctx context.Context, l Loader, r *Remote, lk *lock.Lock) (ServerResult, error) {
	var res ServerResult
	locked := &lk.Loader
	if s := locked.Server; s != nil && s.URL != "" {
		if isServerCached(r.Cache, s) {
			return res, nil
		}
		r.log("downloading %s server files %s", l.Name, locked.Version)
		if _, err := r.Cache.Ensure(ctx, r.Fetch, s.URL, s.Sha512); err != nil {
			return res, err
		}
		res.WasFetched = true
		return res, ensureDownloads(ctx, r, s)
	}
	r.log("downloading %s server files (loader %s)", l.Name, locked.Version)
	url := l.installerURL(r, lk.Minecraft, locked.Version)
	sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
	if err != nil {
		return res, err
	}
	libs, err := installerLibraries(r.Cache.Object(sha))
	if err != nil {
		return res, err
	}
	next := &lock.ServerJar{URL: url, Sha512: sha, Libraries: map[string]lock.Download{}}
	for _, lib := range libs {
		sha, err := r.Cache.FetchChecked(ctx, r.Fetch, lib.url, lib.sha1)
		if err != nil {
			return res, err
		}
		next.Libraries[lib.name] = lock.Download{URL: lib.url, Sha512: sha}
	}
	locked.Server = next
	res.ChangedLock, res.WasFetched = true, true
	return res, nil
}

// installerVanillaServer is where the installer finds the vanilla server jar: under libraries/, or
// in the server dir itself for the older installers.
func installerVanillaServer(l Loader, minecraft string) string {
	if l.RootServerJars {
		return VanillaServerJar(minecraft)
	}
	name := "server-" + minecraft
	if l.MinecraftJarClassifier != "" {
		name += "-" + l.MinecraftJarClassifier
	}
	return "libraries/net/minecraft/server/" + minecraft + "/" + name + ".jar"
}

func installerLaunchArgs(l Loader, lk *lock.Lock) []string {
	if l.RootServerJars {
		return []string{"-jar", l.InstalledServerFile(lk)}
	}
	return []string{"@" + l.InstalledServerFile(lk)}
}

// InstalledServerFile is what the loader's server installer leaves for the server to start from,
// relative to the server directory: an args file, or a jar for the older installers. It is empty
// when the loader has no installer.
func (l Loader) InstalledServerFile(lk *lock.Lock) string {
	if l.InstallServerFlag == "" {
		return ""
	}
	if l.RootServerJars {
		return l.InstalledServerJar(lk.Minecraft, lk.Loader.Version)
	}
	name := "unix_args.txt"
	if runtime.GOOS == "windows" {
		name = "win_args.txt"
	}
	return "libraries/" + l.MavenPath + "/" + l.ArtifactVersion(lk.Minecraft, lk.Loader.Version) + "/" + name
}

type installerLibrary struct {
	name, url, sha1 string
}

// installerLibraries lists what an installer jar downloads: the libraries in its
// install_profile.json and version.json. Libraries without a URL ship inside the installer itself.
func installerLibraries(path string) ([]installerLibrary, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	defer zr.Close()
	byName := map[string]installerLibrary{}
	for _, name := range []string{"install_profile.json", "version.json"} {
		var profile struct {
			Libraries []struct {
				Name      string `json:"name"`
				Downloads struct {
					Artifact struct {
						URL  string `json:"url"`
						Sha1 string `json:"sha1"`
					} `json:"artifact"`
				} `json:"downloads"`
			} `json:"libraries"`
		}
		data, err := readZipFile(zr, name)
		if err != nil {
			return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
		}
		if err := json.Unmarshal(data, &profile); err != nil {
			return nil, unreadable(err, name, "the loader installer %s can't be read", path)
		}
		for _, lib := range profile.Libraries {
			if a := lib.Downloads.Artifact; a.URL != "" {
				byName[lib.Name] = installerLibrary{name: lib.Name, url: a.URL, sha1: a.Sha1}
			}
		}
	}
	libs := make([]installerLibrary, 0, len(byName))
	for _, lib := range byName {
		libs = append(libs, lib)
	}
	sort.Slice(libs, func(i, j int) bool { return libs[i].name < libs[j].name })
	return libs, nil
}

func installerVersionJSON(path string) (json.RawMessage, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	defer zr.Close()
	data, err := readZipFile(zr, "version.json")
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	if !json.Valid(data) {
		return nil, invalid("the loader installer %s holds a version.json that isn't valid JSON", path)
	}
	return data, nil
}

func readZipFile(zr *zip.ReadCloser, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
