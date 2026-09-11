package curseforge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/provider"
)

const (
	APIURL   = "https://api.curseforge.com/v1"
	KeyEnv   = "SHULKER_CURSEFORGE_KEY"
	gameID   = "432"
	classMod = "6"
	pageSize = 50
)

var embeddedKey string

var channels = map[int]string{1: "release", 2: "beta", 3: "alpha"}

var relations = map[int]string{1: "embedded", 2: "optional", 3: "required", 4: "tool", 5: "incompatible", 6: "include"}

type CurseForge struct {
	Client  *fetch.Client
	BaseURL string
	slugs   map[string]string
}

func Key(configured string) string {
	if k := os.Getenv(KeyEnv); k != "" {
		return k
	}
	if configured != "" {
		return configured
	}
	return embeddedKey
}

func New(c *fetch.Client, key string) *CurseForge {
	keyed := *c
	keyed.Header = http.Header{"X-Api-Key": {key}}
	return &CurseForge{Client: &keyed, BaseURL: APIURL, slugs: map[string]string{}}
}

func (c *CurseForge) Name() string { return "curseforge" }

type mod struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Links struct {
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

func (c *CurseForge) Project(ctx context.Context, slugOrID string) (*provider.Project, error) {
	if _, err := strconv.Atoi(slugOrID); err == nil {
		var res struct {
			Data mod `json:"data"`
		}
		found, err := c.Client.GetJSONIfFound(ctx, c.BaseURL+"/mods/"+slugOrID, &res)
		if err != nil {
			return nil, c.wrap("project "+slugOrID, err)
		}
		if !found {
			return nil, fmt.Errorf("curseforge project %s: %w", slugOrID, provider.ErrNotFound)
		}
		return c.remember(res.Data), nil
	}
	q := url.Values{"gameId": {gameID}, "classId": {classMod}, "slug": {slugOrID}}
	var res struct {
		Data []mod `json:"data"`
	}
	if err := c.Client.GetJSON(ctx, c.BaseURL+"/mods/search?"+q.Encode(), &res); err != nil {
		return nil, c.wrap("project "+slugOrID, err)
	}
	for _, m := range res.Data {
		if m.Slug == slugOrID {
			return c.remember(m), nil
		}
	}
	return nil, fmt.Errorf("curseforge project %s: %w", slugOrID, provider.ErrNotFound)
}

func (c *CurseForge) Versions(ctx context.Context, projectID, game, loaderName string) ([]provider.Version, error) {
	l, ok := loader.Lookup(loaderName)
	if !ok {
		return nil, fmt.Errorf("curseforge has no loader type for %q", loaderName)
	}
	var out []provider.Version
	for index := 0; ; {
		q := url.Values{"gameVersion": {game}, "modLoaderType": {l.CurseForgeType}, "index": {strconv.Itoa(index)}, "pageSize": {strconv.Itoa(pageSize)}}
		var res struct {
			Data       []file     `json:"data"`
			Pagination pagination `json:"pagination"`
		}
		if err := c.Client.GetJSON(ctx, c.BaseURL+"/mods/"+url.PathEscape(projectID)+"/files?"+q.Encode(), &res); err != nil {
			return nil, c.wrap("files for "+projectID, err)
		}
		for _, f := range res.Data {
			if !f.IsAvailable || !contains(f.GameVersions, game) || !containsFold(f.GameVersions, loaderName) {
				continue
			}
			v, err := convertFile(f)
			if err != nil {
				continue
			}
			out = append(out, v)
		}
		index += len(res.Data)
		if len(res.Data) == 0 || index >= res.Pagination.TotalCount {
			break
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
	if err := c.Client.PostJSON(ctx, c.BaseURL+"/mods/files", map[string]any{"fileIds": []int{id}}, &res); err != nil {
		return nil, c.wrap("file "+versionID, err)
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
			if _, err := c.Project(ctx, v.ProjectID); err != nil {
				return nil, err
			}
		}
		versions[i].Page = FilePage(c.slugs[v.ProjectID], v.ID)
	}
	return versions, nil
}

func (c *CurseForge) remember(m mod) *provider.Project {
	p := convertMod(m)
	c.slugs[p.ID] = p.Slug
	return p
}

func (c *CurseForge) wrap(what string, err error) error {
	if errors.Is(err, fetch.ErrForbidden) {
		return fmt.Errorf("curseforge %s: the API key was rejected; set %s or curseforge.key in the config file", what, KeyEnv)
	}
	return fmt.Errorf("curseforge %s: %w", what, err)
}

func FilePage(slug, fileID string) string {
	return "https://www.curseforge.com/minecraft/mc-mods/" + slug + "/files/" + fileID
}

func ProjectPage(projectID string) string {
	return "https://www.curseforge.com/projects/" + projectID
}

func convertMod(m mod) *provider.Project {
	return &provider.Project{ID: strconv.Itoa(m.ID), Slug: m.Slug, Title: m.Name}
}

func convertFile(f file) (provider.Version, error) {
	v := provider.Version{
		ID:        strconv.Itoa(f.ID),
		ProjectID: strconv.Itoa(f.ModID),
		Number:    f.DisplayName,
		Channel:   channels[f.ReleaseType],
		File:      provider.File{URL: f.DownloadURL, Filename: f.FileName},
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
