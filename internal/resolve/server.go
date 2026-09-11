package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"maps"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/meta"
)

type ServerJarResult struct {
	Locked  bool
	Fetched bool
}

func (r *Resolver) EnsureServerJar(ctx context.Context, mt *Meta) (ServerJarResult, error) {
	if _, err := loader.Require(r.Lock.Loader.Type); err != nil {
		return ServerJarResult{}, err
	}
	if r.Lock.Loader.Type == "quilt" {
		return r.ensureQuiltServer(ctx, mt)
	}
	return r.ensureFabricServer(ctx, mt.Fabric)
}

func (r *Resolver) ensureFabricServer(ctx context.Context, fabric *meta.Fabric) (ServerJarResult, error) {
	var res ServerJarResult
	l := &r.Lock.Loader
	if l.Server == nil {
		installer, err := fabric.InstallerVersion(ctx)
		if err != nil {
			return res, err
		}
		r.log("downloading fabric server launcher (installer %s)", installer)
		sha, err := r.Cache.Fetch(ctx, r.Fetch, fabric.ServerJarURL(r.Lock.Minecraft, l.Version, installer))
		if err != nil {
			return res, err
		}
		l.Server = &lock.ServerJar{Installer: installer, Sha512: sha}
		res.Locked, res.Fetched = true, true
		return res, nil
	}
	if r.Cache.Has(l.Server.Sha512) {
		return res, nil
	}
	r.log("downloading fabric server launcher (installer %s)", l.Server.Installer)
	if _, err := r.Cache.Ensure(ctx, r.Fetch, fabric.ServerJarURL(r.Lock.Minecraft, l.Version, l.Server.Installer), l.Server.Sha512); err != nil {
		return res, err
	}
	res.Fetched = true
	return res, nil
}

func (r *Resolver) ensureQuiltServer(ctx context.Context, mt *Meta) (ServerJarResult, error) {
	var res ServerJarResult
	l := &r.Lock.Loader
	locked := l.Server
	if locked != nil && (locked.Minecraft == "" || len(locked.Libraries) == 0) {
		locked = nil
	}
	if locked != nil && r.quiltServerCached(locked) {
		return res, nil
	}
	r.log("downloading quilt server (loader %s)", l.Version)
	profile, err := mt.Quilt.ServerProfile(ctx, r.Lock.Minecraft, l.Version)
	if err != nil {
		return res, err
	}
	next := &lock.ServerJar{Libraries: map[string]string{}}
	if locked != nil {
		next.Libraries = maps.Clone(locked.Libraries)
	} else {
		for _, lib := range profile.Libraries {
			next.Libraries[lib.Name] = ""
		}
	}
	urls := map[string]string{}
	for _, lib := range profile.Libraries {
		if urls[lib.Name], err = lib.JarURL(); err != nil {
			return res, err
		}
	}
	for _, name := range sortedKeys(next.Libraries) {
		url, ok := urls[name]
		if !ok {
			return res, fmt.Errorf("quilt meta no longer lists the locked library %s for loader %s", name, l.Version)
		}
		if next.Libraries[name], err = r.ensureFile(ctx, url, next.Libraries[name]); err != nil {
			return res, err
		}
	}
	if next.Minecraft, err = r.ensureVanillaServer(ctx, mt.Piston, locked); err != nil {
		return res, err
	}
	jar, err := quiltLaunchJar(profile.LauncherMainClass, profile.MainClass, sortedKeys(next.Libraries))
	if err != nil {
		return res, err
	}
	if next.Sha512, err = r.Cache.Put(bytes.NewReader(jar)); err != nil {
		return res, err
	}
	if locked != nil && next.Sha512 != locked.Sha512 {
		return res, errors.New("the generated quilt server launch jar doesn't match shulker.lock; remove loader.server from shulker.lock to relock it")
	}
	res.Fetched = true
	if locked == nil {
		l.Server = next
		res.Locked = true
	}
	return res, nil
}

func (r *Resolver) quiltServerCached(s *lock.ServerJar) bool {
	if !r.Cache.Has(s.Sha512) || !r.Cache.Has(s.Minecraft) {
		return false
	}
	for _, sha := range s.Libraries {
		if !r.Cache.Has(sha) {
			return false
		}
	}
	return true
}

func (r *Resolver) ensureFile(ctx context.Context, url, sha string) (string, error) {
	if sha == "" {
		return r.Cache.Fetch(ctx, r.Fetch, url)
	}
	_, err := r.Cache.Ensure(ctx, r.Fetch, url, sha)
	return sha, err
}

func (r *Resolver) ensureVanillaServer(ctx context.Context, piston *meta.Piston, locked *lock.ServerJar) (string, error) {
	if locked != nil && r.Cache.Has(locked.Minecraft) {
		return locked.Minecraft, nil
	}
	dl, err := piston.ServerDownload(ctx, r.Lock.Minecraft)
	if err != nil {
		return "", err
	}
	if locked != nil {
		return r.ensureFile(ctx, dl.URL, locked.Minecraft)
	}
	sha, err := r.Cache.Fetch(ctx, r.Fetch, dl.URL)
	if err != nil {
		return "", err
	}
	got, err := sha1Of(r.Cache.Path(sha))
	if err != nil {
		return "", err
	}
	if got != dl.Sha1 {
		return "", fmt.Errorf("%s: sha1 mismatch (expected %s, got %s)", dl.URL, dl.Sha1, got)
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
