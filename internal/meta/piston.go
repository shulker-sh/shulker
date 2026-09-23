package meta

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io/fs"

	"shulker.sh/shulker/internal/fetch"
)

const PistonManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

// Piston reads Mojang's version manifest and the version JSON each entry points at.
type Piston struct {
	Client      *fetch.Client
	ManifestURL string
}

type GameVersion struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	ReleaseTime string `json:"releaseTime"`
}

type GameManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []GameVersion `json:"versions"`
}

type JavaRuntime struct {
	Component string `json:"component"`
	Major     int    `json:"majorVersion"`
}

func NewPiston(c *fetch.Client) *Piston {
	return &Piston{Client: c, ManifestURL: PistonManifestURL}
}

func (p *Piston) Manifest(ctx context.Context) (*GameManifest, error) {
	var m GameManifest
	if err := p.Client.GetJSON(ctx, p.ManifestURL, &m); err != nil {
		return nil, fetchFailed(err, "mojang", "couldn't read the Minecraft version list")
	}
	return &m, nil
}

func (m *GameManifest) Find(id string) (GameVersion, bool) {
	for _, v := range m.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return GameVersion{}, false
}

type Download struct {
	URL  string `json:"url"`
	Sha1 string `json:"sha1"`
}

func (p *Piston) ServerDownload(ctx context.Context, game string) (Download, error) {
	dl, err := p.serverDownload(ctx, game)
	if err != nil {
		return Download{}, err
	}
	if dl.URL == "" || dl.Sha1 == "" {
		return Download{}, invalid("minecraft %s has no server download", game)
	}
	return dl, nil
}

// serverDownload is the server jar Mojang's version JSON names for game, empty when it names none.
func (p *Piston) serverDownload(ctx context.Context, game string) (Download, error) {
	m, err := p.Manifest(ctx)
	if err != nil {
		return Download{}, err
	}
	v, ok := m.Find(game)
	if !ok {
		return Download{}, notListed(game)
	}
	var detail struct {
		Downloads struct {
			Server Download `json:"server"`
		} `json:"downloads"`
	}
	if err := p.Client.GetJSON(ctx, v.URL, &detail); err != nil {
		return Download{}, versionFetchFailed(err, v.ID)
	}
	return detail.Downloads.Server, nil
}

// Version is a game version's own JSON, as a launcher reads it to install and start the client.
func (p *Piston) Version(ctx context.Context, game string) (json.RawMessage, error) {
	m, err := p.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	v, ok := m.Find(game)
	if !ok {
		return nil, notListed(game)
	}
	var detail json.RawMessage
	if err := p.Client.GetJSON(ctx, v.URL, &detail); err != nil {
		return nil, versionFetchFailed(err, v.ID)
	}
	return detail, nil
}

// DataVersion is a game version's data version, the world_version in the version.json its server jar
// carries; Mojang's version JSON doesn't name it. Only the jar's zip directory and that one entry
// are read, by byte ranges. It is 0 when the version has no server jar or the jar doesn't say.
func (p *Piston) DataVersion(ctx context.Context, game string) (int, error) {
	server, err := p.serverDownload(ctx, game)
	if err != nil || server.URL == "" {
		return 0, err
	}
	jar, err := p.Client.Remote(ctx, server.URL)
	if err != nil {
		return 0, dataVersionFailed(err, game)
	}
	zr, err := zip.NewReader(jar, jar.Size())
	if err != nil {
		return 0, dataVersionFailed(err, game)
	}
	f, err := zr.Open("version.json")
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, dataVersionFailed(err, game)
	}
	defer f.Close()
	var version struct {
		WorldVersion int `json:"world_version"`
	}
	if err := json.NewDecoder(f).Decode(&version); err != nil {
		return 0, dataVersionFailed(err, game)
	}
	return version.WorldVersion, nil
}

func dataVersionFailed(err error, game string) error {
	return fetchFailed(err, "mojang", "couldn't read the minecraft %s data version from its server jar", game)
}

// Java is the runtime component and major version Mojang names for a game version.
func (p *Piston) Java(ctx context.Context, v GameVersion) (JavaRuntime, error) {
	var detail struct {
		JavaVersion JavaRuntime `json:"javaVersion"`
	}
	if err := p.Client.GetJSON(ctx, v.URL, &detail); err != nil {
		return JavaRuntime{}, versionFetchFailed(err, v.ID)
	}
	if detail.JavaVersion.Component == "" {
		return JavaRuntime{}, invalid("minecraft %s names no Java runtime", v.ID)
	}
	return detail.JavaVersion, nil
}

func notListed(game string) error {
	return invalid("minecraft %s is not in Mojang's version list", game)
}

func versionFetchFailed(err error, game string) error {
	return fetchFailed(err, "mojang", "couldn't read the minecraft %s version JSON", game)
}
