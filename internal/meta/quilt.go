package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/fetch"
)

const (
	QuiltMetaURL  = "https://meta.quiltmc.org/v3"
	QuiltMavenURL = "https://maven.quiltmc.org/repository/release"
)

// Quilt reads Quilt's meta and Maven.
type Quilt struct {
	Client   *fetch.Client
	BaseURL  string
	MavenURL string
}

func NewQuilt(c *fetch.Client) *Quilt {
	return &Quilt{Client: c, BaseURL: QuiltMetaURL, MavenURL: QuiltMavenURL}
}

// LoaderJarURL points at the loader jar on Quilt's Maven. Quilt meta's loader hashes don't match
// the jars Maven serves, so callers hash the download themselves.
func (q *Quilt) LoaderJarURL(ctx context.Context, game, loader string) (string, error) {
	var entry struct {
		Loader struct {
			Maven string `json:"maven"`
		} `json:"loader"`
	}
	if err := q.Client.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s", q.BaseURL, game, loader), &entry); err != nil {
		return "", fetchFailed(err, "quilt", "couldn't read Quilt loader %s for minecraft %s", loader, game)
	}
	path, err := MavenPath(entry.Loader.Maven)
	if err != nil {
		return "", invalid("Quilt's meta gives no Maven coordinate for loader %s on minecraft %s", loader, game)
	}
	return q.MavenURL + "/" + path, nil
}

type Library struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ServerProfile struct {
	MainClass         string    `json:"mainClass"`
	LauncherMainClass string    `json:"launcherMainClass"`
	Libraries         []Library `json:"libraries"`
}

// ServerProfile is what a Quilt server launches: its main classes and libraries.
func (q *Quilt) ServerProfile(ctx context.Context, game, loader string) (*ServerProfile, error) {
	var p ServerProfile
	if err := q.Client.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s/server/json", q.BaseURL, game, loader), &p); err != nil {
		return nil, fetchFailed(err, "quilt", "couldn't read the Quilt %s server profile for minecraft %s", loader, game)
	}
	if p.MainClass == "" || p.LauncherMainClass == "" || len(p.Libraries) == 0 {
		return nil, invalid("the Quilt %s server profile for minecraft %s is incomplete", loader, game)
	}
	return &p, nil
}

// LoaderProfile is the launcher profile JSON Quilt's meta serves for a loader on a game version.
func (q *Quilt) LoaderProfile(ctx context.Context, game, loader string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := q.Client.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", q.BaseURL, game, loader), &raw); err != nil {
		return nil, fetchFailed(err, "quilt", "couldn't read the Quilt %s profile for minecraft %s", loader, game)
	}
	return raw, nil
}

func (l Library) JarURL() (string, error) {
	path, err := MavenPath(l.Name)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(l.URL, "/") + "/" + path, nil
}

// MavenPath is where a group:artifact:version[:classifier][@extension] coordinate's file sits under a
// Maven root; without an extension it is a jar.
func MavenPath(name string) (string, error) {
	coords, ext, ok := strings.Cut(name, "@")
	if !ok {
		ext = "jar"
	}
	parts := strings.Split(coords, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", invalid("the Maven coordinate %q is not group:artifact:version[:classifier]", name)
	}
	group, artifact, version := strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	return group + "/" + artifact + "/" + version + "/" + file + "." + ext, nil
}

// LoaderVersions lists every Quilt loader for a game. Quilt meta has no stable flag and doesn't
// sort the list; a version without a prerelease suffix counts as stable.
func (q *Quilt) LoaderVersions(ctx context.Context, game string) ([]LoaderVersion, error) {
	var entries []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := q.Client.GetJSON(ctx, q.BaseURL+"/versions/loader/"+game, &entries); err != nil {
		return nil, fetchFailed(err, "quilt", "couldn't read the Quilt loaders for minecraft %s", game)
	}
	out := make([]LoaderVersion, 0, len(entries))
	for _, e := range entries {
		out = append(out, LoaderVersion{Version: e.Loader.Version, Stable: !strings.Contains(e.Loader.Version, "-")})
	}
	return out, nil
}
