package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/shulker-sh/shulker/internal/fetch"
	"github.com/shulker-sh/shulker/internal/provider"
)

const APIURL = "https://api.modrinth.com/v2"

type Modrinth struct {
	Client  *fetch.Client
	BaseURL string
}

func New(c *fetch.Client) *Modrinth {
	return &Modrinth{Client: c, BaseURL: APIURL}
}

func (m *Modrinth) Name() string { return "modrinth" }

type project struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	ClientSide string `json:"client_side"`
	ServerSide string `json:"server_side"`
}

type version struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	DatePublished string   `json:"date_published"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		VersionID      string `json:"version_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

func (m *Modrinth) Project(ctx context.Context, slugOrID string) (*provider.Project, error) {
	var p project
	if err := m.Client.GetJSON(ctx, m.BaseURL+"/project/"+url.PathEscape(slugOrID), &p); err != nil {
		if errors.Is(err, fetch.ErrNotFound) {
			err = provider.ErrNotFound
		}
		return nil, fmt.Errorf("modrinth project %s: %w", slugOrID, err)
	}
	return &provider.Project{ID: p.ID, Slug: p.Slug, Title: p.Title, Side: side(p.ClientSide, p.ServerSide)}, nil
}

func (m *Modrinth) Versions(ctx context.Context, projectID, game, loader string) ([]provider.Version, error) {
	q := url.Values{}
	q.Set("loaders", jsonList(loader))
	q.Set("game_versions", jsonList(game))
	var raw []version
	if err := m.Client.GetJSON(ctx, m.BaseURL+"/project/"+url.PathEscape(projectID)+"/version?"+q.Encode(), &raw); err != nil {
		return nil, fmt.Errorf("modrinth versions for %s: %w", projectID, err)
	}
	out := make([]provider.Version, 0, len(raw))
	for _, v := range raw {
		pv, err := convert(v)
		if err != nil {
			continue
		}
		out = append(out, pv)
	}
	return out, nil
}

func (m *Modrinth) Version(ctx context.Context, versionID string) (*provider.Version, error) {
	var raw version
	if err := m.Client.GetJSON(ctx, m.BaseURL+"/version/"+url.PathEscape(versionID), &raw); err != nil {
		return nil, fmt.Errorf("modrinth version %s: %w", versionID, err)
	}
	pv, err := convert(raw)
	if err != nil {
		return nil, err
	}
	return &pv, nil
}

func (m *Modrinth) VersionByHash(ctx context.Context, sha1 string) (*provider.Version, bool, error) {
	var raw version
	found, err := m.Client.GetJSONIfFound(ctx, m.BaseURL+"/version_file/"+url.PathEscape(sha1)+"?algorithm=sha1", &raw)
	if err != nil {
		return nil, false, fmt.Errorf("modrinth version_file %s: %w", sha1, err)
	}
	if !found {
		return nil, false, nil
	}
	pv, err := convert(raw)
	if err != nil {
		return nil, false, err
	}
	return &pv, true, nil
}

func convert(v version) (provider.Version, error) {
	pv := provider.Version{ID: v.ID, ProjectID: v.ProjectID, Number: v.VersionNumber, Channel: v.VersionType, GameVersions: v.GameVersions, Loaders: v.Loaders}
	pv.Published, _ = time.Parse(time.RFC3339, v.DatePublished)
	found := false
	for _, f := range v.Files {
		if f.Primary || !found {
			pv.File = provider.File{URL: f.URL, Filename: f.Filename, Sha512: f.Hashes["sha512"]}
			found = true
			if f.Primary {
				break
			}
		}
	}
	if !found || pv.File.Sha512 == "" {
		return pv, fmt.Errorf("modrinth version %s has no downloadable file", v.ID)
	}
	for _, d := range v.Dependencies {
		pv.Dependencies = append(pv.Dependencies, provider.Dependency{ProjectID: d.ProjectID, VersionID: d.VersionID, Type: d.DependencyType})
	}
	return pv, nil
}

func side(client, server string) string {
	switch {
	case client == "unsupported" && server != "unsupported":
		return "server"
	case server == "unsupported" && client != "unsupported":
		return "client"
	}
	return "both"
}

func jsonList(items ...string) string {
	b, _ := json.Marshal(items)
	return string(b)
}
