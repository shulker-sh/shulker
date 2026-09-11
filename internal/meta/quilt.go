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
		return "", fmt.Errorf("quilt loader %s for %s: %w", loader, game, err)
	}
	path, err := MavenPath(entry.Loader.Maven)
	if err != nil {
		return "", fmt.Errorf("quilt loader %s for %s: meta has no maven coordinate", loader, game)
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

func (q *Quilt) ServerProfile(ctx context.Context, game, loader string) (*ServerProfile, error) {
	var p ServerProfile
	if err := q.Client.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s/server/json", q.BaseURL, game, loader), &p); err != nil {
		return nil, fmt.Errorf("quilt server profile for %s with loader %s: %w", game, loader, err)
	}
	if p.MainClass == "" || p.LauncherMainClass == "" || len(p.Libraries) == 0 {
		return nil, fmt.Errorf("quilt server profile for %s with loader %s is incomplete", game, loader)
	}
	return &p, nil
}

func (q *Quilt) LoaderProfile(ctx context.Context, game, loader string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := q.Client.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", q.BaseURL, game, loader), &raw); err != nil {
		return nil, fmt.Errorf("quilt profile for %s with loader %s: %w", game, loader, err)
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

func MavenPath(name string) (string, error) {
	parts := strings.Split(name, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", fmt.Errorf("maven coordinate %q is not group:artifact:version[:classifier]", name)
	}
	group, artifact, version := strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	return group + "/" + artifact + "/" + version + "/" + file + ".jar", nil
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
		return nil, fmt.Errorf("quilt loader versions for %s: %w", game, err)
	}
	out := make([]LoaderVersion, 0, len(entries))
	for _, e := range entries {
		out = append(out, LoaderVersion{Version: e.Loader.Version, Stable: !strings.Contains(e.Loader.Version, "-")})
	}
	return out, nil
}
