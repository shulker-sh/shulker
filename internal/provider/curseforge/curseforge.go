package curseforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

const (
	APIURL   = "https://api.curseforge.com/v1"
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
)

var classTypes = map[int]string{6: "mod", 4471: "modpack", 12: "resourcepack", 6552: "shader"}

var typeClasses = map[string]string{"mod": classMod, "modpack": classModpack, "resourcepack": classResourcePack, "shader": classShader}

var embeddedKey string

var channels = map[int]string{1: "release", 2: "beta", 3: "alpha"}

var relations = map[int]string{1: "embedded", 2: "optional", 3: "required", 4: "tool", 5: "incompatible", 6: "include"}

type CurseForge struct {
	Client  *fetch.Client
	BaseURL string
	KeyURL  string
	// KeyFile is where a key fetched from KeyURL is saved. It is empty for the user's own key,
	// which is never replaced.
	KeyFile    string
	key        string
	refreshed  bool
	refreshErr error
	slugs      map[string]string
	classes    map[string]int
}

func Key(configured string) string {
	if k := os.Getenv(KeyEnv); k != "" {
		return k
	}
	return configured
}

func SharedKey(cacheDir string) string {
	var saved struct {
		Key string `json:"key"`
	}
	if data, err := os.ReadFile(keyFile(cacheDir)); err == nil && json.Unmarshal(data, &saved) == nil && saved.Key != "" {
		return saved.Key
	}
	return embeddedKey
}

func keyFile(cacheDir string) string {
	return filepath.Join(cacheDir, "curseforge-key.json")
}

func New(c *fetch.Client, key string) *CurseForge {
	return &CurseForge{Client: withKey(c, key), BaseURL: APIURL, key: key, slugs: map[string]string{}, classes: map[string]int{}}
}

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

type mod struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	ClassID   int    `json:"classId"`
	Downloads int64  `json:"downloadCount"`
	Links     struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
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
		e := out.Errorf("type-ambiguous", "curseforge has %s as %s; pass `--type` to say which one you mean", slugOrID, strings.Join(kinds, " and "))
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
			q := url.Values{"gameVersion": {game}, "index": {strconv.Itoa(index)}, "pageSize": {strconv.Itoa(pageSize)}}
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
				if seen[f.ID] || !f.IsAvailable || !contains(f.GameVersions, game) {
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

func (c *CurseForge) withPages(ctx context.Context, versions []provider.Version) ([]provider.Version, error) {
	for i, v := range versions {
		if _, ok := c.slugs[v.ProjectID]; !ok {
			if _, err := c.Project(ctx, v.ProjectID, ""); err != nil {
				return nil, err
			}
		}
		versions[i].Page = FilePage(c.slugs[v.ProjectID], v.ID, c.classes[v.ProjectID])
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
	if errors.Is(err, fetch.ErrForbidden) && c.KeyFile != "" {
		if !c.refreshed {
			c.refreshed = true
			if c.refreshErr = c.refreshKey(ctx); c.refreshErr == nil {
				err = request()
			}
		}
		if c.refreshErr != nil {
			return c.refreshErr
		}
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, fetch.ErrForbidden) {
		if c.KeyFile != "" {
			return sharedKeyRejected("the newer one from shulker.sh was rejected too")
		}
		return out.Errorf("curseforge-key-rejected", "curseforge %s: the API key was rejected; set %s or run `shulker config set curseforge.key <key>`", what, KeyEnv)
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
	return out.Errorf("curseforge-key-rejected", "CurseForge rejected shulker's built-in API key and %s; set %s or run `shulker config set curseforge.key <key>`, or report it at https://github.com/shulker-sh/shulker/issues", detail, KeyEnv)
}

// classPaths are the url segments curseforge.com uses per class.
var classPaths = map[int]string{6: "mc-mods", 4471: "modpacks", 12: "texture-packs", 6552: "shaders"}

func FilePage(slug, fileID string, class int) string {
	path, ok := classPaths[class]
	if !ok {
		path = "mc-mods"
	}
	return "https://www.curseforge.com/minecraft/" + path + "/" + slug + "/files/" + fileID
}

func ProjectPage(projectID string) string {
	return "https://www.curseforge.com/projects/" + projectID
}

func convertMod(m mod) *provider.Project {
	return &provider.Project{ID: strconv.Itoa(m.ID), Slug: m.Slug, Title: m.Name, Type: classTypes[m.ClassID], Downloads: m.Downloads}
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
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			v.File.Sha1 = h.Value
		}
	}
	if v.File.Filename == "" || v.File.Sha1 == "" {
		return v, fmt.Errorf("curseforge file %d has no downloadable file", f.ID)
	}
	for _, g := range f.GameVersions {
		if _, isLoader := loader.Lookup(strings.ToLower(g)); isLoader {
			v.Loaders = append(v.Loaders, strings.ToLower(g))
		} else {
			v.GameVersions = append(v.GameVersions, g)
		}
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
