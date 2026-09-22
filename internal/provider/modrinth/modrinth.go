// Package modrinth reads projects and versions from Modrinth's API.
package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

const (
	APIURL      = "https://api.modrinth.com/v2"
	searchLimit = 100
)

// longestWait is the longest rate-limit reset worth waiting out. Modrinth's window is a minute.
const longestWait = time.Minute

type Modrinth struct {
	Client  *fetch.Client
	BaseURL string
	// Log, when set, is told when a request waits out the rate limit.
	Log   func(format string, args ...any)
	sleep func(ctx context.Context, d time.Duration) error
}

func New(c *fetch.Client) *Modrinth {
	return &Modrinth{Client: c, BaseURL: APIURL, sleep: sleep}
}

// call runs request, and when Modrinth rate-limits it, waits out the reset the answer names and
// runs it once more.
func (m *Modrinth) call(ctx context.Context, request func() error) error {
	err := request()
	var se *fetch.StatusError
	if !errors.Is(err, fetch.ErrRateLimited) || !errors.As(err, &se) {
		return err
	}
	if se.RetryAfter <= 0 || se.RetryAfter > longestWait {
		return rateLimited(se.RetryAfter)
	}
	if m.Log != nil {
		m.Log("waiting %s for Modrinth's rate limit", se.RetryAfter)
	}
	if err := m.sleep(ctx, se.RetryAfter); err != nil {
		return err
	}
	if err := request(); !errors.Is(err, fetch.ErrRateLimited) {
		return err
	}
	return rateLimited(longestWait)
}

func rateLimited(reset time.Duration) error {
	e := out.Errorf("rate-limited", "modrinth is rate-limiting shulker's requests")
	e.Help = "run the command again in a minute"
	if reset > longestWait {
		e.Help = fmt.Sprintf("run the command again in %s", reset.Round(time.Minute))
	}
	return e
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (m *Modrinth) Name() string { return "modrinth" }

type project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	ClientSide  string `json:"client_side"`
	ServerSide  string `json:"server_side"`
	ProjectType string `json:"project_type"`
	Downloads   int64  `json:"downloads"`
}

// hit is a search result, which names the project id differently from the
// project itself.
type hit struct {
	ProjectID string `json:"project_id"`
	project
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
		Size     int64             `json:"size"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		VersionID      string `json:"version_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

// Modrinth slugs are unique across project types, so the kind hint is unused.
func (m *Modrinth) Project(ctx context.Context, slugOrID, _ string) (*provider.Project, error) {
	var p project
	if err := m.call(ctx, func() error { return m.Client.GetJSON(ctx, m.BaseURL+"/project/"+url.PathEscape(slugOrID), &p) }); err != nil {
		if errors.Is(err, fetch.ErrNotFound) {
			err = provider.ErrNotFound
		}
		return nil, fmt.Errorf("modrinth project %s: %w", slugOrID, err)
	}
	found := convertProject(p)
	return &found, nil
}

func (m *Modrinth) Search(ctx context.Context, query, kind string, limit int) ([]provider.Project, error) {
	q := url.Values{"query": {query}, "limit": {strconv.Itoa(min(limit, searchLimit))}}
	if kind != "" {
		q.Set("facets", facet("project_type:"+kind))
	}
	var res struct {
		Hits []hit `json:"hits"`
	}
	if err := m.call(ctx, func() error { return m.Client.GetJSON(ctx, m.BaseURL+"/search?"+q.Encode(), &res) }); err != nil {
		return nil, fmt.Errorf("modrinth search %s: %w", query, err)
	}
	projects := make([]provider.Project, 0, len(res.Hits))
	for _, h := range res.Hits {
		found := convertProject(h.project)
		found.ID = h.ProjectID
		projects = append(projects, found)
	}
	return projects, nil
}

func convertProject(p project) provider.Project {
	return provider.Project{ID: p.ID, Slug: p.Slug, Title: p.Title, Side: side(p.ClientSide, p.ServerSide), Type: p.ProjectType, Downloads: p.Downloads}
}

func (m *Modrinth) Versions(ctx context.Context, projectID, game string, loaders []string) ([]provider.Version, error) {
	q := url.Values{}
	if len(loaders) > 0 {
		q.Set("loaders", jsonList(loaders...))
	}
	if game != "" {
		q.Set("game_versions", jsonList(game))
	}
	var raw []version
	if err := m.call(ctx, func() error {
		return m.Client.GetJSON(ctx, m.BaseURL+"/project/"+url.PathEscape(projectID)+"/version?"+q.Encode(), &raw)
	}); err != nil {
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
	var found bool
	err := m.call(ctx, func() (err error) {
		found, err = m.Client.GetJSONIfFound(ctx, m.BaseURL+"/version/"+url.PathEscape(versionID), &raw)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("modrinth version %s: %w", versionID, err)
	}
	if !found {
		return nil, fmt.Errorf("modrinth version %s: %w", versionID, provider.ErrNotFound)
	}
	pv, err := convert(raw)
	if err != nil {
		return nil, err
	}
	return &pv, nil
}

// VersionsByHash finds the versions whose files have these sha1s, in one request, by sha1. A hash
// Modrinth doesn't know is left out.
func (m *Modrinth) VersionsByHash(ctx context.Context, sha1s []string) (map[string]provider.Version, error) {
	raw := map[string]version{}
	if err := m.call(ctx, func() error {
		return m.Client.PostJSON(ctx, m.BaseURL+"/version_files", map[string]any{"hashes": sha1s, "algorithm": "sha1"}, &raw)
	}); err != nil {
		return nil, fmt.Errorf("modrinth version_files: %w", err)
	}
	found := make(map[string]provider.Version, len(raw))
	for sha1, v := range raw {
		if pv, err := convert(v); err == nil {
			found[sha1] = pv
		}
	}
	return found, nil
}

// Projects finds these projects in one request, by id. A project Modrinth no longer has is left
// out.
func (m *Modrinth) Projects(ctx context.Context, ids []string) (map[string]provider.Project, error) {
	var raw []project
	if err := m.call(ctx, func() error {
		return m.Client.GetJSON(ctx, m.BaseURL+"/projects?"+url.Values{"ids": {jsonList(ids...)}}.Encode(), &raw)
	}); err != nil {
		return nil, fmt.Errorf("modrinth projects: %w", err)
	}
	found := make(map[string]provider.Project, len(raw))
	for _, p := range raw {
		found[p.ID] = convertProject(p)
	}
	return found, nil
}

func convert(v version) (provider.Version, error) {
	pv := provider.Version{ID: v.ID, ProjectID: v.ProjectID, Number: v.VersionNumber, Channel: v.VersionType, GameVersions: v.GameVersions, Loaders: v.Loaders}
	pv.Published, _ = time.Parse(time.RFC3339, v.DatePublished)
	found := false
	for _, f := range v.Files {
		if f.Primary || !found {
			pv.File = provider.File{URL: f.URL, Filename: f.Filename, Sha512: f.Hashes["sha512"], Sha1: f.Hashes["sha1"], Size: f.Size}
			found = true
			if f.Primary {
				break
			}
		}
	}
	if !found || pv.File.Sha512 == "" {
		return pv, out.Errorf("version-no-file", "modrinth version %s has no file shulker can download", v.ID)
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

// facet wraps one filter the way Modrinth reads a requirement: the array of
// alternatives that must match, inside the array of requirements.
func facet(f string) string {
	b, _ := json.Marshal([][]string{{f}})
	return string(b)
}

func jsonList(items ...string) string {
	b, _ := json.Marshal(items)
	return string(b)
}
