package loader

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/zipfile"
)

// quiltEnsureServer assembles a Quilt server from its meta server profile: the libraries it lists
// and a deterministic launch jar, since Quilt's launcher never downloads anything itself.
func quiltEnsureServer(ctx context.Context, _ Loader, r *Remote, lk *lock.Lock) (ServerResult, error) {
	var res ServerResult
	q := newQuiltMeta(r)
	l := &lk.Loader
	if locked := l.Server; locked != nil && len(locked.Libraries) > 0 {
		if isServerCached(r.Cache, locked) {
			return res, nil
		}
		r.log("downloading the quilt server %s", l.Version)
		if err := ensureDownloads(ctx, r, locked); err != nil {
			return res, err
		}
		res.WasFetched = true
		if r.Cache.Has(locked.Sha512) {
			return res, nil
		}
		profile, err := q.serverProfile(ctx, lk.Minecraft, l.Version)
		if err != nil {
			return res, err
		}
		sha, err := putQuiltLaunchJar(r, profile, locked.Libraries)
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
	profile, err := q.serverProfile(ctx, lk.Minecraft, l.Version)
	if err != nil {
		return res, err
	}
	next := &lock.ServerJar{Libraries: map[string]lock.Download{}}
	for _, lib := range profile.Libraries {
		url, err := lib.jarURL()
		if err != nil {
			return res, err
		}
		sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
		if err != nil {
			return res, err
		}
		next.Libraries[lib.Name] = lock.Download{URL: url, Sha512: sha}
	}
	if next.Sha512, err = putQuiltLaunchJar(r, profile, next.Libraries); err != nil {
		return res, err
	}
	l.Server = next
	res.ChangedLock, res.WasFetched = true, true
	return res, nil
}

func putQuiltLaunchJar(r *Remote, profile *quiltServerProfile, libraries map[string]lock.Download) (string, error) {
	jar, err := quiltLaunchJar(profile.LauncherMainClass, profile.MainClass, slices.Sorted(maps.Keys(libraries)))
	if err != nil {
		return "", err
	}
	return r.Cache.Put(bytes.NewReader(jar))
}

func quiltLaunchJar(launcherMainClass, mainClass string, libraries []string) ([]byte, error) {
	classPath := make([]string, len(libraries))
	for i, name := range libraries {
		path, err := MavenPath(name)
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

	return zipfile.Build(map[string][]byte{
		"META-INF/MANIFEST.MF":           []byte(manifest.String()),
		"quilt-server-launch.properties": []byte("launch.mainClass=" + mainClass + "\n"),
	}, "")
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
