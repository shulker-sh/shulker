package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"hash/crc32"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
)

type ServerJarResult struct {
	ChangedLock bool
	WasFetched  bool
}

// EnsureServerJar caches the vanilla and loader server jars, locking any the lock doesn't have yet.
func (r *Resolver) EnsureServerJar(ctx context.Context, mt *Meta) (ServerJarResult, error) {
	res, err := r.ensureLoaderServer(ctx, mt)
	if err != nil {
		return res, err
	}
	vanilla, err := r.ensureVanillaServer(ctx, mt.Piston)
	res.ChangedLock = res.ChangedLock || vanilla.ChangedLock
	res.WasFetched = res.WasFetched || vanilla.WasFetched
	return res, err
}

func (r *Resolver) ensureLoaderServer(ctx context.Context, mt *Meta) (ServerJarResult, error) {
	if r.Lock.Loader.Type == "" {
		return ServerJarResult{}, nil
	}
	l, err := loader.Require(r.Lock.Loader.Type)
	if err != nil {
		return ServerJarResult{}, err
	}
	switch {
	case l.Name == "quilt":
		return r.ensureQuiltServer(ctx, mt)
	case l.InstallServerFlag == "":
		return r.ensureFabricServer(ctx, mt.Fabric)
	}
	url, err := mt.InstallerURL(r.Lock)
	if err != nil {
		return ServerJarResult{}, err
	}
	return r.ensureInstallerServer(ctx, mt, url)
}

func (r *Resolver) ensureVanillaServer(ctx context.Context, piston *meta.Piston) (ServerJarResult, error) {
	var res ServerJarResult
	if locked := r.Lock.Server; locked != nil {
		if r.Cache.Has(locked.Sha512) {
			return res, nil
		}
		r.log("downloading the Minecraft %s server", r.Lock.Minecraft)
		if _, err := r.Cache.Ensure(ctx, r.Fetch, locked.URL, locked.Sha512); err != nil {
			return res, err
		}
		res.WasFetched = true
		return res, nil
	}
	r.log("downloading the Minecraft %s server", r.Lock.Minecraft)
	dl, err := piston.ServerDownload(ctx, r.Lock.Minecraft)
	if err != nil {
		return res, err
	}
	sha, err := r.fetchChecked(ctx, dl.URL, dl.Sha1)
	if err != nil {
		return res, err
	}
	r.Lock.Server = &lock.Download{URL: dl.URL, Sha512: sha}
	res.ChangedLock, res.WasFetched = true, true
	return res, nil
}

type InstallerJar struct {
	Path        string
	ChangedLock bool
}

// EnsureClientInstaller caches the loader's own installer jar for a client install and locks it as
// loader.client, so later links verify it and hit the cache. The client libraries the installer
// downloads itself stay unlocked, so it runs online.
func (r *Resolver) EnsureClientInstaller(ctx context.Context, mt *Meta) (InstallerJar, error) {
	l := &r.Lock.Loader
	if locked := l.Client; locked != nil {
		path, err := r.Cache.Ensure(ctx, r.Fetch, locked.URL, locked.Sha512)
		return InstallerJar{Path: path}, err
	}
	url, err := mt.InstallerURL(r.Lock)
	if err != nil {
		return InstallerJar{}, err
	}
	if s := l.Server; s != nil && s.URL == url && r.Cache.Has(s.Sha512) {
		l.Client = &lock.Download{URL: url, Sha512: s.Sha512}
		return InstallerJar{Path: r.Cache.Object(s.Sha512), ChangedLock: true}, nil
	}
	r.log("downloading the %s installer %s", l.Type, l.Version)
	sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
	if err != nil {
		return InstallerJar{}, err
	}
	l.Client = &lock.Download{URL: url, Sha512: sha}
	return InstallerJar{Path: r.Cache.Object(sha), ChangedLock: true}, nil
}

// ensureInstallerServer locks a loader's own installer jar plus everything it would download: the
// libraries its profiles list. The build places those and the vanilla server jar, so the installer
// runs offline and only builds what it generates.
func (r *Resolver) ensureInstallerServer(ctx context.Context, mt *Meta, installerURL string) (ServerJarResult, error) {
	var res ServerJarResult
	l := &r.Lock.Loader
	if locked := l.Server; locked != nil && locked.URL != "" {
		if r.isServerCached(locked) {
			return res, nil
		}
		r.log("downloading %s server files %s", l.Type, l.Version)
		if _, err := r.Cache.Ensure(ctx, r.Fetch, locked.URL, locked.Sha512); err != nil {
			return res, err
		}
		res.WasFetched = true
		return res, r.ensureDownloads(ctx, locked)
	}
	r.log("downloading %s server files (loader %s)", l.Type, l.Version)
	sha, err := r.Cache.Fetch(ctx, r.Fetch, installerURL)
	if err != nil {
		return res, err
	}
	libs, err := meta.InstallerLibraries(r.Cache.Object(sha))
	if err != nil {
		return res, err
	}
	next := &lock.ServerJar{URL: installerURL, Sha512: sha, Libraries: map[string]lock.Download{}}
	for _, lib := range libs {
		sha, err := r.fetchChecked(ctx, lib.URL, lib.Sha1)
		if err != nil {
			return res, err
		}
		next.Libraries[lib.Name] = lock.Download{URL: lib.URL, Sha512: sha}
	}
	l.Server = next
	res.ChangedLock, res.WasFetched = true, true
	return res, nil
}

func (r *Resolver) ensureFabricServer(ctx context.Context, fabric *meta.Fabric) (ServerJarResult, error) {
	var res ServerJarResult
	l := &r.Lock.Loader
	if l.Server == nil {
		installer, err := fabric.InstallerVersion(ctx)
		if err != nil {
			return res, err
		}
		r.log("downloading the fabric server launcher %s", installer)
		url := fabric.ServerJarURL(r.Lock.Minecraft, l.Version, installer)
		sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
		if err != nil {
			return res, err
		}
		l.Server = &lock.ServerJar{Installer: installer, URL: url, Sha512: sha}
		res.ChangedLock, res.WasFetched = true, true
		return res, nil
	}
	if l.Server.URL == "" {
		l.Server.URL = fabric.ServerJarURL(r.Lock.Minecraft, l.Version, l.Server.Installer)
		res.ChangedLock = true
	}
	if r.Cache.Has(l.Server.Sha512) {
		return res, nil
	}
	r.log("downloading the fabric server launcher %s", l.Server.Installer)
	if _, err := r.Cache.Ensure(ctx, r.Fetch, l.Server.URL, l.Server.Sha512); err != nil {
		return res, err
	}
	res.WasFetched = true
	return res, nil
}

func (r *Resolver) ensureQuiltServer(ctx context.Context, mt *Meta) (ServerJarResult, error) {
	var res ServerJarResult
	l := &r.Lock.Loader
	if locked := l.Server; locked != nil && len(locked.Libraries) > 0 {
		if r.isServerCached(locked) {
			return res, nil
		}
		r.log("downloading the quilt server %s", l.Version)
		if err := r.ensureDownloads(ctx, locked); err != nil {
			return res, err
		}
		res.WasFetched = true
		if r.Cache.Has(locked.Sha512) {
			return res, nil
		}
		profile, err := mt.Quilt.ServerProfile(ctx, r.Lock.Minecraft, l.Version)
		if err != nil {
			return res, err
		}
		sha, err := r.putQuiltLaunchJar(profile, locked.Libraries)
		if err != nil {
			return res, err
		}
		if sha != locked.Sha512 {
			e := out.Errorf("lock-stale", "the generated quilt server launch jar doesn't match shulker.lock")
			e.Help = "remove loader.server from shulker.lock to relock it"
			return res, e
		}
		return res, nil
	}
	r.log("downloading quilt server (loader %s)", l.Version)
	profile, err := mt.Quilt.ServerProfile(ctx, r.Lock.Minecraft, l.Version)
	if err != nil {
		return res, err
	}
	next := &lock.ServerJar{Libraries: map[string]lock.Download{}}
	for _, lib := range profile.Libraries {
		url, err := lib.JarURL()
		if err != nil {
			return res, err
		}
		sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
		if err != nil {
			return res, err
		}
		next.Libraries[lib.Name] = lock.Download{URL: url, Sha512: sha}
	}
	if next.Sha512, err = r.putQuiltLaunchJar(profile, next.Libraries); err != nil {
		return res, err
	}
	l.Server = next
	res.ChangedLock, res.WasFetched = true, true
	return res, nil
}

func (r *Resolver) putQuiltLaunchJar(profile *meta.ServerProfile, libraries map[string]lock.Download) (string, error) {
	jar, err := quiltLaunchJar(profile.LauncherMainClass, profile.MainClass, sortedKeys(libraries))
	if err != nil {
		return "", err
	}
	return r.Cache.Put(bytes.NewReader(jar))
}

func (r *Resolver) isServerCached(s *lock.ServerJar) bool {
	if !r.Cache.Has(s.Sha512) {
		return false
	}
	for _, dl := range s.Libraries {
		if !r.Cache.Has(dl.Sha512) {
			return false
		}
	}
	return true
}

func (r *Resolver) ensureDownloads(ctx context.Context, s *lock.ServerJar) error {
	downloads := make([]lock.Download, 0, len(s.Libraries))
	for _, name := range sortedKeys(s.Libraries) {
		downloads = append(downloads, s.Libraries[name])
	}
	for _, dl := range downloads {
		if _, err := r.Cache.Ensure(ctx, r.Fetch, dl.URL, dl.Sha512); err != nil {
			return err
		}
	}
	return nil
}

// fetchChecked downloads a file the first time it is locked, checking the sha1 its source publishes.
func (r *Resolver) fetchChecked(ctx context.Context, url, sha1 string) (string, error) {
	sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
	if err != nil || sha1 == "" {
		return sha, err
	}
	got, err := fsutil.SHA1(r.Cache.Object(sha))
	if err != nil {
		return "", err
	}
	if got != sha1 {
		e := out.Errorf("checksum-mismatch", "the download from %s doesn't match the sha1 its metadata gives", url)
		e.Rows = []out.Detail{{Label: "want", Text: sha1}, {Label: "got", Text: got}}
		return "", e
	}
	return sha, nil
}

func quiltLaunchJar(launcherMainClass, mainClass string, libraries []string) ([]byte, error) {
	classPath := make([]string, len(libraries))
	for i, name := range libraries {
		path, err := meta.MavenPath(name)
		if err != nil {
			return nil, err
		}
		classPath[i] = "libraries/" + path
	}
	sort.Strings(classPath)
	var manifest strings.Builder
	manifestLine(&manifest, "Manifest-Version", "1.0")
	manifestLine(&manifest, "Main-Class", launcherMainClass)
	manifestLine(&manifest, "Class-Path", strings.Join(classPath, " "))
	manifest.WriteString("\r\n")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, data string }{
		{"META-INF/MANIFEST.MF", manifest.String()},
		{"quilt-server-launch.properties", "launch.mainClass=" + mainClass + "\n"},
	} {
		// Stored with fixed times so every machine generates the jar the lock hashed.
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name:               f.name,
			Method:             zip.Store,
			Modified:           time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			CRC32:              crc32.ChecksumIEEE([]byte(f.data)),
			CompressedSize64:   uint64(len(f.data)),
			UncompressedSize64: uint64(len(f.data)),
		})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.data)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Jar manifests cap lines at 72 bytes; longer values continue on lines starting with a space.
func manifestLine(b *strings.Builder, name, value string) {
	line := name + ": " + value
	for len(line) > 72 {
		b.WriteString(line[:72] + "\r\n")
		line = " " + line[72:]
	}
	b.WriteString(line + "\r\n")
}
