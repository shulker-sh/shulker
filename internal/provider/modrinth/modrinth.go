// Package modrinth reads projects and versions from Modrinth's API.
package modrinth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/integrations"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

const (
	APIURL      = "https://api.modrinth.com/v2"
	siteURL     = "https://modrinth.com"
	searchLimit = 100
)

var (
	hosts = []string{"modrinth.com", "www.modrinth.com"}
	// sections are the modrinth.com/<section>/<slug> pages a project has.
	sections = []string{"mod", "project", "plugin", "resourcepack", "shader", "datapack", "modpack"}
)

// vanillaShader is the loader tag of a shader that is really a resource pack of core shaders. It is
// no shader mod, so it passes through to the lock as it is.
const vanillaShader = "vanilla"

// longestWait is the longest rate-limit reset worth waiting out. Modrinth's window is a minute.
const longestWait = time.Minute

type Modrinth struct {
	Client  *fetch.Client
	BaseURL string
	// Log, when set, is told when a request waits out the rate limit.
	Log   func(format string, args ...any)
	sleep func(ctx context.Context, d time.Duration) error
}

var _ provider.Provider = (*Modrinth)(nil)

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
	e := out.Errorf("rate-limited", "Modrinth is rate-limiting shulker's requests")
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

func (m *Modrinth) Title() string { return "Modrinth" }

func (m *Modrinth) Hosts() []string { return []string{"modrinth.com"} }

func (m *Modrinth) Available() error { return nil }

// Modrinth reads a slug straight, so a slug finds its project as surely as its id.
func (m *Modrinth) KeysBySlug() bool { return true }

func (m *Modrinth) NamesVersions() bool { return false }

func (m *Modrinth) NotFoundHelp() string { return "" }

// PackTags are the loader tags Modrinth files each pack kind's versions under: resource packs
// under minecraft, datapacks under datapack and shaders under the shader loader they target.
func (m *Modrinth) PackTags(kind string) []string {
	switch kind {
	case manifest.TypeShader:
		return append(integrations.ShaderTags(m.Name()), vanillaShader)
	case manifest.TypeDatapack:
		return []string{provider.DatapackLoader}
	}
	return []string{"minecraft"}
}

type project struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ClientSide  string   `json:"client_side"`
	ServerSide  string   `json:"server_side"`
	ProjectType string   `json:"project_type"`
	Downloads   int64    `json:"downloads"`
	Loaders     []string `json:"loaders"`
}

// hit is a search result, which names the project id differently from the
// project itself.
type hit struct {
	ProjectID  string   `json:"project_id"`
	Author     string   `json:"author"`
	Categories []string `json:"categories"`
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
		found.Author = h.Author
		found.Datapack = slices.Contains(h.Categories, provider.DatapackLoader)
		projects = append(projects, found)
	}
	return projects, nil
}

func convertProject(p project) provider.Project {
	kind := p.ProjectType
	if kind == "mod" && len(p.Loaders) > 0 && !slices.ContainsFunc(p.Loaders, func(l string) bool { return l != provider.DatapackLoader }) {
		kind = provider.DatapackLoader
	}
	return provider.Project{ID: p.ID, Slug: p.Slug, Title: p.Title, Summary: p.Description, Side: side(p.ClientSide, p.ServerSide), Type: kind, Datapack: slices.Contains(p.Loaders, provider.DatapackLoader), Downloads: p.Downloads, Page: projectPage(p.ProjectType, p.Slug)}
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
	return m.version(ctx, "/version/"+url.PathEscape(versionID), versionID)
}

// ProjectVersion reads one of a project's versions by its id or its version number.
func (m *Modrinth) ProjectVersion(ctx context.Context, project, version string) (*provider.Version, error) {
	return m.version(ctx, "/project/"+url.PathEscape(project)+"/version/"+url.PathEscape(version), project+" "+version)
}

func (m *Modrinth) version(ctx context.Context, path, name string) (*provider.Version, error) {
	var raw version
	var found bool
	err := m.call(ctx, func() (err error) {
		found, err = m.Client.GetJSONIfFound(ctx, m.BaseURL+path, &raw)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("modrinth version %s: %w", name, err)
	}
	if !found {
		return nil, fmt.Errorf("modrinth version %s: %w", name, provider.ErrNotFound)
	}
	pv, err := convert(raw)
	if err != nil {
		return nil, err
	}
	return &pv, nil
}

func (m *Modrinth) VersionsByID(ctx context.Context, ids []string) (map[string]provider.Version, map[string]error, error) {
	var raw []version
	if err := m.call(ctx, func() error {
		return m.Client.GetJSON(ctx, m.BaseURL+"/versions?"+url.Values{"ids": {jsonList(ids...)}}.Encode(), &raw)
	}); err != nil {
		return nil, nil, fmt.Errorf("modrinth versions: %w", err)
	}
	found := make(map[string]provider.Version, len(raw))
	unusable := map[string]error{}
	for _, v := range raw {
		pv, err := convert(v)
		if err != nil {
			unusable[v.ID] = err
			continue
		}
		found[v.ID] = pv
	}
	return found, unusable, nil
}

// Identify looks the files up by sha1, then their projects, in one request each, whatever the
// count, since Modrinth rate-limits by the request.
func (m *Modrinth) Identify(ctx context.Context, files map[string][]byte) (map[string]provider.Hosted, error) {
	sha1s := make(map[string]string, len(files))
	for key, data := range files {
		sum := sha1.Sum(data)
		sha1s[key] = hex.EncodeToString(sum[:])
	}
	return m.IdentifySHA1(ctx, sha1s)
}

func (m *Modrinth) IdentifySHA1(ctx context.Context, sha1s map[string]string) (map[string]provider.Hosted, error) {
	hashes := slices.Sorted(maps.Values(sha1s))
	if len(hashes) == 0 {
		return map[string]provider.Hosted{}, nil
	}
	versions, err := m.versionsByHash(ctx, slices.Compact(hashes))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, v := range versions {
		ids = append(ids, v.ProjectID)
	}
	found := map[string]provider.Hosted{}
	if len(ids) == 0 {
		return found, nil
	}
	slices.Sort(ids)
	projects, err := m.Projects(ctx, slices.Compact(ids))
	if err != nil {
		return nil, err
	}
	for key, sha1Sum := range sha1s {
		v, hosted := versions[sha1Sum]
		proj, hasProject := projects[v.ProjectID]
		if hosted && hasProject {
			found[key] = provider.Hosted{Project: proj, Version: v}
		}
	}
	return found, nil
}

// Filed asks version_files for every file by its sha512, in one request.
func (m *Modrinth) Filed(ctx context.Context, files map[string]provider.LockedFile) (map[string]provider.Filing, []string, error) {
	found := map[string]provider.Filing{}
	var hashes, unchecked []string
	for _, key := range slices.Sorted(maps.Keys(files)) {
		if sum := files[key].Sha512; sum != "" {
			hashes = append(hashes, sum)
		} else {
			unchecked = append(unchecked, key)
		}
	}
	if len(hashes) == 0 {
		return found, unchecked, nil
	}
	slices.Sort(hashes)
	raw := map[string]version{}
	if err := m.call(ctx, func() error {
		return m.Client.PostJSON(ctx, m.BaseURL+"/version_files", map[string]any{"hashes": slices.Compact(hashes), "algorithm": "sha512"}, &raw)
	}); err != nil {
		return nil, nil, fmt.Errorf("modrinth version_files: %w", err)
	}
	for key, f := range files {
		if v, ok := raw[f.Sha512]; ok && f.Sha512 != "" {
			found[key] = provider.Filing{Project: v.ProjectID, Version: v.ID}
		}
	}
	return found, unchecked, nil
}

// versionsByHash finds the versions whose files have these sha1s, in one request. A hash Modrinth
// doesn't know is left out.
func (m *Modrinth) versionsByHash(ctx context.Context, sha1s []string) (map[string]provider.Version, error) {
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

// loaders are a version's loader tags, with a shader's shader mods named by integration id.
func loaders(tags []string) []string {
	ids := integrations.ShadersTagged("modrinth", tags)
	if len(ids) == 0 {
		return tags
	}
	if slices.Contains(tags, vanillaShader) {
		ids = append(ids, vanillaShader)
	}
	return ids
}

func convert(v version) (provider.Version, error) {
	pv := provider.Version{ID: v.ID, ProjectID: v.ProjectID, Number: v.VersionNumber, Channel: v.VersionType, GameVersions: v.GameVersions, Loaders: loaders(v.Loaders)}
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
		return pv, out.Errorf("version-no-file", "Modrinth version %s has no file shulker can download", v.ID)
	}
	for _, d := range v.Dependencies {
		pv.Dependencies = append(pv.Dependencies, provider.Dependency{ProjectID: d.ProjectID, VersionID: d.VersionID, Type: d.DependencyType})
	}
	return pv, nil
}

func (m *Modrinth) ParseURL(u *url.URL) (provider.Ref, error) {
	host := strings.ToLower(u.Hostname())
	var parts []string
	for part := range strings.SplitSeq(u.Path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	switch {
	case slices.Contains(hosts, host):
		if len(parts) >= 2 && slices.Contains(sections, parts[0]) {
			if len(parts) == 2 {
				return provider.Ref{Project: parts[1]}, nil
			}
			if len(parts) == 4 && parts[2] == "version" {
				return provider.Ref{Project: parts[1], Version: parts[3]}, nil
			}
		}
	case host == "cdn.modrinth.com":
		if len(parts) == 5 && parts[0] == "data" && parts[2] == "versions" {
			return provider.Ref{Project: parts[1], Version: parts[3]}, nil
		}
	default:
		return provider.Ref{}, provider.ErrNotHosted
	}
	return provider.Ref{}, out.Errorf("usage", "Modrinth can't read %s", u)
}

func (m *Modrinth) URLShapes() []string {
	return []string{
		siteURL + "/<" + strings.Join(sections, "|") + ">/<slug or id>[/version/<id or number>]",
		"https://cdn.modrinth.com/data/<project>/versions/<version>/<file>",
	}
}

func (m *Modrinth) ProjectPage(kind, slugOrID string) string { return projectPage(kind, slugOrID) }

func (m *Modrinth) VersionsPage(kind, slug string) string {
	return projectPage(kind, slug) + "/versions"
}

func projectPage(kind, slugOrID string) string {
	if kind == "" {
		kind = "project"
	}
	return siteURL + "/" + kind + "/" + slugOrID
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
