package meta

import (
	"context"
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

type LoaderJar struct {
	URL    string
	Sha512 string
}

func NewQuilt(c *fetch.Client) *Quilt {
	return &Quilt{Client: c, BaseURL: QuiltMetaURL, MavenURL: QuiltMavenURL}
}

func (q *Quilt) LoaderJar(ctx context.Context, game, loader string) (LoaderJar, error) {
	var entry struct {
		Loader struct {
			Maven  string `json:"maven"`
			Hashes struct {
				Sha512 string `json:"sha512"`
			} `json:"hashes"`
		} `json:"loader"`
	}
	if err := q.Client.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s", q.BaseURL, game, loader), &entry); err != nil {
		return LoaderJar{}, fmt.Errorf("quilt loader %s for %s: %w", loader, game, err)
	}
	parts := strings.Split(entry.Loader.Maven, ":")
	if len(parts) != 3 || entry.Loader.Hashes.Sha512 == "" {
		return LoaderJar{}, fmt.Errorf("quilt loader %s for %s: meta has no maven coordinate or sha512", loader, game)
	}
	group, artifact, version := strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2]
	return LoaderJar{
		URL:    fmt.Sprintf("%s/%s/%s/%s/%s-%s.jar", q.MavenURL, group, artifact, version, artifact, version),
		Sha512: entry.Loader.Hashes.Sha512,
	}, nil
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
