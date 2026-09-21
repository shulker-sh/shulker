package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
)

const (
	runtimeMarkerFile   = ".shulker-runtime.json"
	runtimeDownloadJobs = 8
)

type Runtime struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	Home      string `json:"home"`
	Fetched   bool   `json:"fetched"`
}

type runtimeMarker struct {
	Component    string `json:"component"`
	Version      string `json:"version"`
	ManifestSha1 string `json:"manifestSha1"`
	Home         string `json:"home"`
}

type RuntimeOptions struct {
	Refresh bool
	Log     func(format string, args ...any)
}

func RuntimeDir(cacheDir, component string) string {
	return filepath.Join(cacheDir, "java", component)
}

func EnsureRuntime(ctx context.Context, client *fetch.Client, runtimes *meta.Runtimes, cacheDir, component string, opts RuntimeOptions) (Runtime, error) {
	dir := RuntimeDir(cacheDir, component)
	existing, hasExisting := readRuntimeMarker(dir)
	if hasExisting && !opts.Refresh {
		return Runtime{Component: component, Version: existing.Version, Home: filepath.Join(dir, existing.Home)}, nil
	}
	platform, ok := meta.RuntimePlatform()
	if !ok {
		return Runtime{}, out.Errorf("runtime-unavailable", "Mojang publishes no Java runtime for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	release, ok, err := runtimes.Release(ctx, platform, component)
	if err != nil {
		return Runtime{}, err
	}
	if !ok {
		return Runtime{}, out.Errorf("runtime-unavailable", "Mojang publishes no %s runtime for %s", component, platform)
	}
	if hasExisting && existing.ManifestSha1 == release.ManifestSha1 {
		return Runtime{Component: component, Version: existing.Version, Home: filepath.Join(dir, existing.Home)}, nil
	}
	files, err := runtimes.Files(ctx, release)
	if err != nil {
		return Runtime{}, err
	}
	home, err := runtimeHome(files)
	if err != nil {
		return Runtime{}, err
	}
	if opts.Log != nil {
		opts.Log("downloading Java runtime %s (%d files, %d MB)", release.Version, countFiles(files), totalSize(files)/(1<<20))
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Runtime{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), component+".tmp-")
	if err != nil {
		return Runtime{}, err
	}
	defer os.RemoveAll(tmp)
	if err := materializeRuntime(ctx, client, tmp, files); err != nil {
		return Runtime{}, err
	}
	marker := runtimeMarker{Component: component, Version: release.Version, ManifestSha1: release.ManifestSha1, Home: home}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return Runtime{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, runtimeMarkerFile), append(data, '\n'), 0o644); err != nil {
		return Runtime{}, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return Runtime{}, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		return Runtime{}, err
	}
	return Runtime{Component: component, Version: release.Version, Home: filepath.Join(dir, home), Fetched: true}, nil
}

func readRuntimeMarker(dir string) (runtimeMarker, bool) {
	data, err := os.ReadFile(filepath.Join(dir, runtimeMarkerFile))
	if err != nil {
		return runtimeMarker{}, false
	}
	var m runtimeMarker
	if err := json.Unmarshal(data, &m); err != nil || m.Home == "" {
		return runtimeMarker{}, false
	}
	return m, true
}

func runtimeHome(files map[string]meta.RuntimeFile) (string, error) {
	for name, f := range files {
		if f.Type != "file" {
			continue
		}
		if strings.HasSuffix(name, "bin/java") || strings.HasSuffix(name, "bin/java.exe") {
			return filepath.Dir(filepath.Dir(filepath.FromSlash(name))), nil
		}
	}
	return "", fmt.Errorf("java runtime manifest lists no bin/java")
}

func countFiles(files map[string]meta.RuntimeFile) int {
	n := 0
	for _, f := range files {
		if f.Type == "file" {
			n++
		}
	}
	return n
}

func totalSize(files map[string]meta.RuntimeFile) int64 {
	var n int64
	for _, f := range files {
		if f.Type == "file" {
			n += f.Downloads.Raw.Size
		}
	}
	return n
}

func materializeRuntime(ctx context.Context, client *fetch.Client, root string, files map[string]meta.RuntimeFile) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var downloads, links []string
	for _, name := range names {
		path, err := runtimePath(root, name)
		if err != nil {
			return err
		}
		switch files[name].Type {
		case "directory":
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case "file":
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			downloads = append(downloads, name)
		case "link":
			links = append(links, name)
		default:
			return fmt.Errorf("java runtime manifest: unknown entry type %q for %s", files[name].Type, name)
		}
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtimeDownloadJobs)
	for _, name := range downloads {
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			path, _ := runtimePath(root, name)
			return downloadRuntimeFile(ctx, client, path, files[name])
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	for _, name := range links {
		path, _ := runtimePath(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.FromSlash(files[name].Target), path); err != nil {
			return err
		}
	}
	return nil
}

func runtimePath(root, name string) (string, error) {
	rel := filepath.Clean(filepath.FromSlash(name))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("java runtime manifest: refusing path %q", name)
	}
	return filepath.Join(root, rel), nil
}

func downloadRuntimeFile(ctx context.Context, client *fetch.Client, path string, f meta.RuntimeFile) error {
	mode := os.FileMode(0o644)
	if f.Executable {
		mode = 0o755
	}
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	h := sha1.New()
	_, err = client.Download(ctx, f.Downloads.Raw.URL, io.MultiWriter(dst, h))
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.Downloads.Raw.Sha1 {
		return fmt.Errorf("%s: sha1 mismatch (expected %s, got %s)", f.Downloads.Raw.URL, f.Downloads.Raw.Sha1, got)
	}
	return nil
}
