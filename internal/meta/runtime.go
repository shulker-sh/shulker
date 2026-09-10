package meta

import (
	"context"
	"fmt"
	"runtime"

	"github.com/andrewmast/shulker/internal/fetch"
)

const RuntimeIndexURL = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

type Runtimes struct {
	Client   *fetch.Client
	IndexURL string
}

type RuntimeRelease struct {
	Version      string
	ManifestURL  string
	ManifestSha1 string
}

type RuntimeFile struct {
	Type       string `json:"type"`
	Executable bool   `json:"executable"`
	Target     string `json:"target"`
	Downloads  struct {
		Raw RuntimeDownload `json:"raw"`
	} `json:"downloads"`
}

type RuntimeDownload struct {
	Sha1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type runtimeIndexEntry struct {
	Manifest struct {
		Sha1 string `json:"sha1"`
		URL  string `json:"url"`
	} `json:"manifest"`
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
}

func NewRuntimes(c *fetch.Client) *Runtimes {
	return &Runtimes{Client: c, IndexURL: RuntimeIndexURL}
}

func RuntimePlatform() (string, bool) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/amd64":
		return "mac-os", true
	case "darwin/arm64":
		return "mac-os-arm64", true
	case "linux/amd64":
		return "linux", true
	case "linux/386":
		return "linux-i386", true
	case "windows/amd64":
		return "windows-x64", true
	case "windows/386":
		return "windows-x86", true
	case "windows/arm64":
		return "windows-arm64", true
	}
	return "", false
}

func (r *Runtimes) Release(ctx context.Context, platform, component string) (RuntimeRelease, bool, error) {
	var index map[string]map[string][]runtimeIndexEntry
	if err := r.Client.GetJSON(ctx, r.IndexURL, &index); err != nil {
		return RuntimeRelease{}, false, fmt.Errorf("java runtime index: %w", err)
	}
	entries := index[platform][component]
	if len(entries) == 0 {
		return RuntimeRelease{}, false, nil
	}
	e := entries[0]
	return RuntimeRelease{Version: e.Version.Name, ManifestURL: e.Manifest.URL, ManifestSha1: e.Manifest.Sha1}, true, nil
}

func (r *Runtimes) Files(ctx context.Context, release RuntimeRelease) (map[string]RuntimeFile, error) {
	var m struct {
		Files map[string]RuntimeFile `json:"files"`
	}
	if err := r.Client.GetJSON(ctx, release.ManifestURL, &m); err != nil {
		return nil, fmt.Errorf("java runtime manifest: %w", err)
	}
	return m.Files, nil
}
