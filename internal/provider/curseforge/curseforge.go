// Package curseforge reads projects and files from CurseForge's API, with either the user's own
// API key or the shared one shulker ships and refreshes from shulker.sh.
package curseforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/integrations"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

const (
	APIURL   = "https://api.curseforge.com/v1"
	siteURL  = "https://www.curseforge.com"
	KeyURL   = "https://shulker.sh/api/curseforge-key"
	KeyEnv   = "SHULKER_CURSEFORGE_KEY"
	gameID   = "432"
	pageSize = 50
	// sortPopularity is CurseForge's Popularity sort field, from /v1/mods/search's
	// sortField enum.
	sortPopularity = "2"
)

// CurseForge class ids for the Minecraft game, from /v1/categories?classesOnly=true.
const (
	classMod          = "6"
	classModpack      = "4471"
	classResourcePack = "12"
	classShader       = "6552"
	classDatapack     = "6945"
)

var classTypes = map[int]string{6: "mod", 4471: "modpack", 12: "resourcepack", 6552: "shader", 6945: "datapack"}

var typeClasses = map[string]string{"mod": classMod, "modpack": classModpack, "resourcepack": classResourcePack, "shader": classShader, "datapack": classDatapack}

// sections are the curseforge.com/minecraft/<section> of each kind shulker can add.
var sections = map[string]string{
	manifest.TypeMod:          "mc-mods",
	manifest.TypeModpack:      "modpacks",
	manifest.TypeResourcePack: "texture-packs",
	manifest.TypeShader:       "shaders",
	manifest.TypeDatapack:     "data-packs",
}

var hosts = []string{"curseforge.com", "www.curseforge.com", "legacy.curseforge.com"}

var embeddedKey string

var channels = map[int]string{1: "release", 2: "beta", 3: "alpha"}

var relations = map[int]string{1: "embedded", 2: "optional", 3: "required", 4: "tool", 5: "incompatible", 6: "include"}

type CurseForge struct {
	Client  *fetch.Client
	BaseURL string
	KeyURL  string
	// KeyFile is where a key fetched from KeyURL is saved. It is empty for the user's own key,
	// which is never replaced.
	KeyFile      string
	key          string
	hasRefreshed bool
	refreshErr   error
	// keyWorked is set once a request has succeeded with the key, after which a 403 is CurseForge
	// locking shulker out rather than the key being wrong.
	keyWorked bool
	slugs     map[string]string
	classes   map[string]int
}

// Key is the user's own API key: SHULKER_CURSEFORGE_KEY when it is set, else the configured one.
// It is empty when the user has none.
func Key(configured string) string {
	if k := os.Getenv(KeyEnv); k != "" {
		return k
	}
	return configured
}

// SharedKey is shulker's shared API key: the one last fetched from shulker.sh into cacheDir, or
// the one built into the binary.
func SharedKey(cacheDir string) string {
	var saved struct {
		Key string `json:"key"`
	}
	if data, err := os.ReadFile(keyFile(cacheDir)); err == nil && json.Unmarshal(data, &saved) == nil && saved.Key != "" {
		return saved.Key
	}
	return embeddedKey
}

// Keys is every API key shulker may have sent: the user's own from the environment and from config,
// and the shared one, fetched and built in. None of them is empty.
func Keys(configured, cacheDir string) []string {
	candidates := []string{os.Getenv(KeyEnv), configured, embeddedKey}
	if cacheDir != "" {
		candidates = append(candidates, SharedKey(cacheDir))
	}
	var keys []string
	for _, k := range candidates {
		if k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return keys
}

func keyFile(cacheDir string) string {
	return filepath.Join(cacheDir, "curseforge-key.json")
}

var _ provider.Provider = (*CurseForge)(nil)

// Open is the CurseForge provider for this machine: the user's own key when there is one, else
// the shared one, replaced from shulker.sh when CurseForge rejects it. With no key at all it is
// unavailable.
func Open(c *fetch.Client, configured, cacheDir string) *CurseForge {
	if key := Key(configured); key != "" {
		return New(c, key)
	}
	return NewShared(c, SharedKey(cacheDir), cacheDir)
}

func New(c *fetch.Client, key string) *CurseForge {
	return &CurseForge{Client: withKey(c, key), BaseURL: APIURL, key: key, slugs: map[string]string{}, classes: map[string]int{}}
}

// NewShared is a CurseForge that uses the shared key, and replaces it from shulker.sh when
// CurseForge rejects it.
func NewShared(c *fetch.Client, key, cacheDir string) *CurseForge {
	cf := New(c, key)
	cf.KeyURL = KeyURL
	cf.KeyFile = keyFile(cacheDir)
	return cf
}

func withKey(c *fetch.Client, key string) *fetch.Client {
	keyed := *c
	keyed.Header = http.Header{"X-Api-Key": {key}}
	return &keyed
}

func (c *CurseForge) Name() string { return "curseforge" }

func (c *CurseForge) Title() string { return "CurseForge" }

func (c *CurseForge) Hosts() []string { return []string{"forgecdn.net", "curseforge.com"} }

func (c *CurseForge) Available() error {
	if c.key != "" {
		return nil
	}
	e := out.Errorf("provider-unavailable", "curseforge needs an API key")
	e.Help = "set " + KeyEnv + " or run `shulker config set curseforge.key <key>`"
	return e
}

// CurseForge finds a slug through its search, which leaves some projects out, so a manifest
// names a project by its id.
func (c *CurseForge) KeysBySlug() bool { return false }

func (c *CurseForge) NamesVersions() bool { return true }

func (c *CurseForge) NotFoundHelp() string {
	return "CurseForge's search doesn't list every project; add one it misses by a file URL (" + siteURL + "/minecraft/mc-mods/<slug>/files/<file id>), by " + siteURL + "/projects/<project id>, or by its project id, shown on its CurseForge page under About Project, with `--provider curseforge`"
}

// PackTags are the shader loaders CurseForge tags shader files with, in gameVersions beside the
// game versions; resource packs and datapacks carry no loader tag at all.
func (c *CurseForge) PackTags(kind string) []string {
	if kind == manifest.TypeShader {
		return integrations.ShaderTags(c.Name())
	}
	return nil
}

type mod struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Summary   string `json:"summary"`
	ClassID   int    `json:"classId"`
	Downloads int64  `json:"downloadCount"`
	Links     struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

type file struct {
	ID           int      `json:"id"`
	ModID        int      `json:"modId"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	ReleaseType  int      `json:"releaseType"`
	FileDate     string   `json:"fileDate"`
	DownloadURL  string   `json:"downloadUrl"`
	FileLength   int64    `json:"fileLength"`
	IsAvailable  bool     `json:"isAvailable"`
	GameVersions []string `json:"gameVersions"`
	ServerPack   int      `json:"serverPackFileId"`
	Hashes       []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	Dependencies []struct {
		ModID        int `json:"modId"`
		RelationType int `json:"relationType"`
	} `json:"dependencies"`
}

type pagination struct {
	Index       int `json:"index"`
	ResultCount int `json:"resultCount"`
	TotalCount  int `json:"totalCount"`
}

func (c *CurseForge) Project(ctx context.Context, slugOrID, kind string) (*provider.Project, error) {
	if _, err := strconv.Atoi(slugOrID); err == nil {
		var res struct {
			Data mod `json:"data"`
		}
		var found bool
		if err := c.call(ctx, "project "+slugOrID, func() (err error) {
			found, err = c.Client.GetJSONIfFound(ctx, c.BaseURL+"/mods/"+slugOrID, &res)
			return err
		}); err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("curseforge project %s: %w", slugOrID, provider.ErrNotFound)
		}
		return c.remember(res.Data), nil
	}
	q := url.Values{"gameId": {gameID}, "slug": {slugOrID}}
	if class, ok := typeClasses[kind]; ok {
		q.Set("classId", class)
	}
	var res struct {
		Data []mod `json:"data"`
	}
	if err := c.call(ctx, "project "+slugOrID, func() error {
		return c.Client.GetJSON(ctx, c.BaseURL+"/mods/search?"+q.Encode(), &res)
	}); err != nil {
		return nil, err
	}
	var matched []mod
	for _, m := range res.Data {
		if m.Slug == slugOrID {
			matched = append(matched, m)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("curseforge project %s: %w", slugOrID, provider.ErrNotFound)
	}
	if len(matched) > 1 {
		kinds := make([]string, 0, len(matched))
		for _, m := range matched {
			kinds = append(kinds, classTypes[m.ClassID])
		}
		sort.Strings(kinds)
		e := out.Errorf("type-ambiguous", "curseforge has %s as %s", slugOrID, strings.Join(kinds, " and "))
		e.Help = "pass `--type` to say which one you mean"
		e.Candidates, e.Given, e.Flag = kinds, slugOrID, "--type"
		return nil, e
	}
	return c.remember(matched[0]), nil
}

func (c *CurseForge) Search(ctx context.Context, query, kind string, limit int) ([]provider.Project, error) {
	q := url.Values{
		"gameId": {gameID}, "searchFilter": {query}, "sortField": {sortPopularity},
		"sortOrder": {"desc"}, "pageSize": {strconv.Itoa(min(limit, pageSize))},
	}
	if class, ok := typeClasses[kind]; ok {
		q.Set("classId", class)
	}
	var res struct {
		Data []mod `json:"data"`
	}
	if err := c.call(ctx, "search "+query, func() error {
		return c.Client.GetJSON(ctx, c.BaseURL+"/mods/search?"+q.Encode(), &res)
	}); err != nil {
		return nil, err
	}
	projects := make([]provider.Project, 0, len(res.Data))
	for _, m := range res.Data {
		// Minecraft has classes shulker has no entry type for, worlds and
		// bukkit plugins among them, and a search without a classId spans them all.
		if _, ok := classTypes[m.ClassID]; !ok {
			continue
		}
		projects = append(projects, *c.remember(m))
	}
	return projects, nil
}

func (c *CurseForge) Versions(ctx context.Context, projectID, game string, loaders []string) ([]provider.Version, error) {
	var out []provider.Version
	seen := map[int]bool{}
	tags := loaders
	if len(tags) == 0 {
		// A resource pack's files carry no loader tag at all.
		tags = []string{""}
	}
	for _, name := range tags {
		for index := 0; ; {
			q := url.Values{"index": {strconv.Itoa(index)}, "pageSize": {strconv.Itoa(pageSize)}}
			if game != "" {
				q.Set("gameVersion", game)
			}
			// Mod loaders filter server-side; shader loaders are tagged in
			// gameVersions instead, alongside the Minecraft versions.
			if l, ok := loader.Lookup(name); ok {
				q.Set("modLoaderType", l.CurseForgeType)
			}
			var res struct {
				Data       []file     `json:"data"`
				Pagination pagination `json:"pagination"`
			}
			if err := c.call(ctx, "files for "+projectID, func() error {
				return c.Client.GetJSON(ctx, c.BaseURL+"/mods/"+url.PathEscape(projectID)+"/files?"+q.Encode(), &res)
			}); err != nil {
				return nil, err
			}
			for _, f := range res.Data {
				if seen[f.ID] || !f.IsAvailable || (game != "" && !contains(f.GameVersions, game)) {
					continue
				}
				if name != "" && !containsFold(f.GameVersions, name) {
					continue
				}
				v, err := convertFile(f)
				if err != nil {
					continue
				}
				seen[f.ID] = true
				out = append(out, v)
			}
			index += len(res.Data)
			if len(res.Data) == 0 || index >= res.Pagination.TotalCount {
				break
			}
		}
	}
	return c.withPages(ctx, out)
}

func (c *CurseForge) Version(ctx context.Context, versionID string) (*provider.Version, error) {
	id, err := strconv.Atoi(versionID)
	if err != nil {
		return nil, fmt.Errorf("curseforge file id %q is not a number", versionID)
	}
	var res struct {
		Data []file `json:"data"`
	}
	if err := c.call(ctx, "file "+versionID, func() error {
		return c.Client.PostJSON(ctx, c.BaseURL+"/mods/files", map[string]any{"fileIds": []int{id}}, &res)
	}); err != nil {
		return nil, err
	}
	if len(res.Data) == 0 {
		return nil, fmt.Errorf("curseforge file %s: %w", versionID, provider.ErrNotFound)
	}
	v, err := convertFile(res.Data[0])
	if err != nil {
		return nil, err
	}
	list, err := c.withPages(ctx, []provider.Version{v})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

// CurseForge names a file by its id alone, so the project adds nothing to the lookup.
func (c *CurseForge) ProjectVersion(ctx context.Context, _, version string) (*provider.Version, error) {
	return c.Version(ctx, version)
}

func (c *CurseForge) Projects(ctx context.Context, ids []string) (map[string]provider.Project, error) {
	mods, err := c.mods(ctx, numbers(ids))
	if err != nil {
		return nil, err
	}
	found := make(map[string]provider.Project, len(mods))
	for id, p := range mods {
		found[strconv.Itoa(id)] = *p
	}
	return found, nil
}

func (c *CurseForge) VersionsByID(ctx context.Context, ids []string) (map[string]provider.Version, map[string]error, error) {
	files, unusableFiles, err := c.files(ctx, numbers(ids))
	if err != nil {
		return nil, nil, err
	}
	found := make(map[string]provider.Version, len(files))
	for id, v := range files {
		found[strconv.Itoa(id)] = v
	}
	unusable := make(map[string]error, len(unusableFiles))
	for id, err := range unusableFiles {
		unusable[strconv.Itoa(id)] = err
	}
	return found, unusable, nil
}

// Identify fingerprints the files and looks the exact matches up, then their projects and files
// by id, in one request each.
func (c *CurseForge) Identify(ctx context.Context, files map[string][]byte) (map[string]provider.Hosted, error) {
	prints := make(map[string]uint32, len(files))
	var all []uint32
	for _, key := range slices.Sorted(maps.Keys(files)) {
		prints[key] = Fingerprint(files[key])
		all = append(all, prints[key])
	}
	found := map[string]provider.Hosted{}
	if len(all) == 0 {
		return found, nil
	}
	matches, err := c.matchFingerprints(ctx, all)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return found, nil
	}
	var modIDs, fileIDs []int
	for _, m := range matches {
		modIDs = append(modIDs, m.ModID)
		fileIDs = append(fileIDs, m.FileID)
	}
	slices.Sort(modIDs)
	slices.Sort(fileIDs)
	projects, err := c.mods(ctx, slices.Compact(modIDs))
	if err != nil {
		return nil, err
	}
	versions, _, err := c.files(ctx, slices.Compact(fileIDs))
	if err != nil {
		return nil, err
	}
	for key, print := range prints {
		m, matched := matches[print]
		if !matched {
			continue
		}
		proj, hasProject := projects[m.ModID]
		v, hasFile := versions[m.FileID]
		if hasProject && hasFile {
			found[key] = provider.Hosted{Project: *proj, Version: v}
		}
	}
	return found, nil
}

// IdentifySHA1 finds nothing: CurseForge indexes files by its own fingerprint, not by sha1.
func (c *CurseForge) IdentifySHA1(context.Context, map[string]string) (map[string]provider.Hosted, error) {
	return map[string]provider.Hosted{}, nil
}

// numbers are the ids CurseForge has, which are integers; any other id is left out.
func numbers(ids []string) []int {
	var ns []int
	for _, id := range ids {
		if n, err := strconv.Atoi(id); err == nil {
			ns = append(ns, n)
		}
	}
	return ns
}

// mods finds these projects in one request, by id. A project CurseForge doesn't have is left out.
func (c *CurseForge) mods(ctx context.Context, ids []int) (map[int]*provider.Project, error) {
	var res struct {
		Data []mod `json:"data"`
	}
	if err := c.call(ctx, "projects", func() error {
		return c.Client.PostJSON(ctx, c.BaseURL+"/mods", map[string]any{"modIds": ids}, &res)
	}); err != nil {
		return nil, err
	}
	found := make(map[int]*provider.Project, len(res.Data))
	for _, m := range res.Data {
		found[m.ID] = c.remember(m)
	}
	return found, nil
}

// files finds these files in one request, by id. A file CurseForge doesn't have is left out, and
// one it has nothing to download for is in unusable with the reason. Call mods for their projects
// first, or each file's page costs a request of its own.
func (c *CurseForge) files(ctx context.Context, ids []int) (found map[int]provider.Version, unusable map[int]error, err error) {
	var res struct {
		Data []file `json:"data"`
	}
	if err := c.call(ctx, "files", func() error {
		return c.Client.PostJSON(ctx, c.BaseURL+"/mods/files", map[string]any{"fileIds": ids}, &res)
	}); err != nil {
		return nil, nil, err
	}
	var versions []provider.Version
	unusable = map[int]error{}
	for _, f := range res.Data {
		v, err := convertFile(f)
		if err != nil {
			unusable[f.ID] = err
			continue
		}
		versions = append(versions, v)
	}
	if versions, err = c.withPages(ctx, versions); err != nil {
		return nil, nil, err
	}
	found = make(map[int]provider.Version, len(versions))
	for _, v := range versions {
		id, _ := strconv.Atoi(v.ID)
		found[id] = v
	}
	return found, unusable, nil
}

func (c *CurseForge) withPages(ctx context.Context, versions []provider.Version) ([]provider.Version, error) {
	for i, v := range versions {
		if _, ok := c.slugs[v.ProjectID]; !ok {
			if _, err := c.Project(ctx, v.ProjectID, ""); err != nil {
				return nil, err
			}
		}
		versions[i].Page = filePage(c.slugs[v.ProjectID], v.ID, c.classes[v.ProjectID])
	}
	return versions, nil
}

func (c *CurseForge) remember(m mod) *provider.Project {
	p := convertMod(m)
	c.slugs[p.ID] = p.Slug
	c.classes[p.ID] = m.ClassID
	return p
}

// call runs request, and when CurseForge rejects shulker's shared key, replaces it from KeyURL
// once per run and retries.
func (c *CurseForge) call(ctx context.Context, what string, request func() error) error {
	err := request()
	if errors.Is(err, fetch.ErrForbidden) && !c.keyWorked && c.KeyFile != "" {
		if !c.hasRefreshed {
			c.hasRefreshed = true
			if c.refreshErr = c.refreshKey(ctx); c.refreshErr == nil {
				err = request()
			}
		}
		if c.refreshErr != nil {
			return c.refreshErr
		}
	}
	if err == nil {
		c.keyWorked = true
		return nil
	}
	if errors.Is(err, fetch.ErrRateLimited) || c.keyWorked && errors.Is(err, fetch.ErrForbidden) {
		e := out.Errorf("rate-limited", "curseforge is rate-limiting shulker's requests")
		e.Help = "CurseForge doesn't say for how long, and reports put it at an hour or more; run the command again later"
		return e
	}
	if errors.Is(err, fetch.ErrForbidden) {
		if c.KeyFile != "" {
			return sharedKeyRejected("the newer one from shulker.sh was rejected too")
		}
		e := out.Errorf("curseforge-key-rejected", "curseforge %s: the API key was rejected", what)
		e.Help = "set " + KeyEnv + " or run `shulker config set curseforge.key <key>`"
		return e
	}
	return fmt.Errorf("curseforge %s: %w", what, err)
}

func (c *CurseForge) refreshKey(ctx context.Context) error {
	client := *c.Client
	client.Header = http.Header{"X-Shulker-Client": {"cli"}}
	var res struct {
		Key string `json:"key"`
	}
	if err := client.GetJSON(ctx, c.KeyURL, &res); err != nil {
		return sharedKeyRejected(fmt.Sprintf("getting a new one from shulker.sh failed (%v)", err))
	}
	if res.Key == "" || res.Key == c.key {
		return sharedKeyRejected("shulker.sh has no newer one")
	}
	if err := os.MkdirAll(filepath.Dir(c.KeyFile), 0o755); err != nil {
		return fmt.Errorf("saving the CurseForge key from shulker.sh: %w", err)
	}
	if err := fsutil.WriteJSON(c.KeyFile, map[string]string{"key": res.Key}); err != nil {
		return fmt.Errorf("saving the CurseForge key from shulker.sh: %w", err)
	}
	c.key = res.Key
	c.Client = withKey(c.Client, res.Key)
	return nil
}

func sharedKeyRejected(detail string) error {
	e := out.Errorf("curseforge-key-rejected", "curseforge rejected shulker's built-in API key and %s", detail)
	e.Help = "set " + KeyEnv + " or run `shulker config set curseforge.key <key>`, or report it at https://github.com/shulker-sh/shulker/issues"
	return e
}

func (c *CurseForge) ParseURL(u *url.URL) (provider.Ref, error) {
	if !slices.Contains(hosts, strings.ToLower(u.Hostname())) {
		return provider.Ref{}, provider.ErrNotHosted
	}
	var parts []string
	for part := range strings.SplitSeq(u.Path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	numeric := func(s string) bool {
		_, err := strconv.Atoi(s)
		return err == nil
	}
	switch {
	case len(parts) == 2 && parts[0] == "projects" && numeric(parts[1]):
		return provider.Ref{Project: parts[1]}, nil
	case len(parts) < 3 || parts[0] != "minecraft" || !slices.Contains(slices.Collect(maps.Values(sections)), parts[1]):
	case len(parts) == 3:
		return provider.Ref{Project: parts[2]}, nil
	case len(parts) == 5 && (parts[3] == "files" || parts[3] == "download") && numeric(parts[4]):
		return provider.Ref{Project: parts[2], Version: parts[4]}, nil
	}
	return provider.Ref{}, out.Errorf("usage", "curseforge can't read %s", u)
}

func (c *CurseForge) URLShapes() []string {
	return []string{
		"https://[www.|legacy.]curseforge.com/minecraft/<" + strings.Join(slices.Sorted(maps.Values(sections)), "|") + ">/<slug>[/files/<file id> or /download/<file id>]",
		"https://[www.|legacy.]curseforge.com/projects/<project id>",
	}
}

// ProjectPage is the project's page: by id, the redirect that needs no section, since a project
// id may name a project whose slug the search can't find.
func (c *CurseForge) ProjectPage(kind, slugOrID string) string {
	if _, err := strconv.Atoi(slugOrID); err == nil {
		return siteURL + "/projects/" + slugOrID
	}
	return siteURL + "/minecraft/" + section(kind) + "/" + slugOrID
}

func (c *CurseForge) VersionsPage(kind, slug string) string {
	return siteURL + "/minecraft/" + section(kind) + "/" + slug + "/files"
}

func section(kind string) string {
	if path, ok := sections[kind]; ok {
		return path
	}
	return sections[manifest.TypeMod]
}

func filePage(slug, fileID string, class int) string {
	return siteURL + "/minecraft/" + section(classTypes[class]) + "/" + slug + "/files/" + fileID
}

func convertMod(m mod) *provider.Project {
	p := &provider.Project{ID: strconv.Itoa(m.ID), Slug: m.Slug, Title: m.Name, Summary: m.Summary, Type: classTypes[m.ClassID], Datapack: m.ClassID == 6945, Downloads: m.Downloads, Page: m.Links.WebsiteURL}
	if len(m.Authors) > 0 {
		p.Author = m.Authors[0].Name
	}
	return p
}

func convertFile(f file) (provider.Version, error) {
	v := provider.Version{
		ID:        strconv.Itoa(f.ID),
		ProjectID: strconv.Itoa(f.ModID),
		Number:    f.DisplayName,
		Channel:   channels[f.ReleaseType],
		File:      provider.File{URL: f.DownloadURL, Filename: f.FileName, Size: f.FileLength},
	}
	v.Published, _ = time.Parse(time.RFC3339, f.FileDate)
	if f.ServerPack != 0 {
		v.ServerPack = strconv.Itoa(f.ServerPack)
	}
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			v.File.Sha1 = h.Value
		}
	}
	if v.File.Filename == "" || v.File.Sha1 == "" {
		return v, out.Errorf("version-no-file", "curseforge file %d has no file shulker can download", f.ID)
	}
	for _, g := range f.GameVersions {
		if _, isLoader := loader.Lookup(strings.ToLower(g)); isLoader {
			v.Loaders = append(v.Loaders, strings.ToLower(g))
		} else {
			v.GameVersions = append(v.GameVersions, g)
		}
	}
	if shaders := integrations.ShadersTagged("curseforge", f.GameVersions); len(shaders) > 0 {
		v.Loaders = shaders
	}
	for _, d := range f.Dependencies {
		v.Dependencies = append(v.Dependencies, provider.Dependency{ProjectID: strconv.Itoa(d.ModID), Type: relations[d.RelationType]})
	}
	return v, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}
