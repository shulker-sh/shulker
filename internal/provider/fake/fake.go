// Package fake is an in-memory provider for tests: projects and versions given up front, files
// identified by sha1, and URLs on <name>.test.
package fake

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

type Provider struct {
	name string
	// Known are the projects the provider has, and Files their versions.
	Known []provider.Project
	Files []provider.Version
	// Label, when set, is the title; the name with its first letter raised otherwise.
	Label string
	// Unavailable, when set, is what Available answers.
	Unavailable error
	// Help, when set, is what a project the provider lacks points the user at.
	Help string
	// KeyedByID says a slug is no sure key on this provider, as on CurseForge, so an entry
	// records the project id.
	KeyedByID bool
	// Requests counts the lookups made, by method name.
	Requests map[string]int
}

func New(name string) *Provider {
	return &Provider{name: name, Requests: map[string]int{}}
}

var _ provider.Provider = (*Provider)(nil)

func (p *Provider) Name() string { return p.name }

func (p *Provider) Title() string {
	if p.Label != "" {
		return p.Label
	}
	return strings.ToUpper(p.name[:1]) + p.name[1:]
}

func (p *Provider) Available() error { return p.Unavailable }

func (p *Provider) KeysBySlug() bool { return !p.KeyedByID }

func (p *Provider) NotFoundHelp() string { return p.Help }

func (p *Provider) PackTags(string) []string { return nil }

func (p *Provider) Project(_ context.Context, slugOrID, kind string) (*provider.Project, error) {
	p.Requests["Project"]++
	for _, proj := range p.Known {
		if (proj.ID == slugOrID || proj.Slug == slugOrID) && (kind == "" || proj.Type == kind) {
			found := proj
			return &found, nil
		}
	}
	return nil, fmt.Errorf("%s project %s: %w", p.name, slugOrID, provider.ErrNotFound)
}

func (p *Provider) Projects(_ context.Context, ids []string) (map[string]provider.Project, error) {
	p.Requests["Projects"]++
	found := map[string]provider.Project{}
	for _, proj := range p.Known {
		if slices.Contains(ids, proj.ID) {
			found[proj.ID] = proj
		}
	}
	return found, nil
}

func (p *Provider) Search(_ context.Context, query, kind string, limit int) ([]provider.Project, error) {
	p.Requests["Search"]++
	var hits []provider.Project
	for _, proj := range p.Known {
		if kind != "" && proj.Type != kind {
			continue
		}
		if strings.Contains(strings.ToLower(proj.Title+" "+proj.Slug), strings.ToLower(query)) {
			hits = append(hits, proj)
		}
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (p *Provider) Versions(_ context.Context, projectID, game string, loaders []string) ([]provider.Version, error) {
	p.Requests["Versions"]++
	var found []provider.Version
	for _, v := range p.Files {
		if v.ProjectID != projectID || (game != "" && !slices.Contains(v.GameVersions, game)) {
			continue
		}
		if len(loaders) > 0 && !slices.ContainsFunc(loaders, func(l string) bool { return slices.Contains(v.Loaders, l) }) {
			continue
		}
		found = append(found, v)
	}
	return found, nil
}

func (p *Provider) Version(_ context.Context, versionID string) (*provider.Version, error) {
	p.Requests["Version"]++
	for _, v := range p.Files {
		if v.ID == versionID {
			found := v
			return &found, nil
		}
	}
	return nil, fmt.Errorf("%s version %s: %w", p.name, versionID, provider.ErrNotFound)
}

func (p *Provider) ProjectVersion(_ context.Context, project, version string) (*provider.Version, error) {
	p.Requests["ProjectVersion"]++
	if i := slices.IndexFunc(p.Known, func(proj provider.Project) bool { return proj.Slug == project }); i >= 0 {
		project = p.Known[i].ID
	}
	for _, v := range p.Files {
		if v.ProjectID == project && (v.ID == version || v.Number == version) {
			found := v
			return &found, nil
		}
	}
	return nil, fmt.Errorf("%s version %s %s: %w", p.name, project, version, provider.ErrNotFound)
}

func (p *Provider) VersionsByID(_ context.Context, ids []string) (map[string]provider.Version, map[string]error, error) {
	p.Requests["VersionsByID"]++
	found := map[string]provider.Version{}
	unusable := map[string]error{}
	for _, v := range p.Files {
		if !slices.Contains(ids, v.ID) {
			continue
		}
		if v.File.Filename == "" {
			unusable[v.ID] = out.Errorf("version-no-file", "%s version %s has no file shulker can download", p.name, v.ID)
			continue
		}
		found[v.ID] = v
	}
	return found, unusable, nil
}

func (p *Provider) Identify(_ context.Context, files map[string][]byte) (map[string]provider.Hosted, error) {
	p.Requests["Identify"]++
	sha1s := make(map[string]string, len(files))
	for key, data := range files {
		sum := sha1.Sum(data)
		sha1s[key] = hex.EncodeToString(sum[:])
	}
	return p.identifySHA1(sha1s), nil
}

func (p *Provider) IdentifySHA1(_ context.Context, sha1s map[string]string) (map[string]provider.Hosted, error) {
	p.Requests["IdentifySHA1"]++
	return p.identifySHA1(sha1s), nil
}

func (p *Provider) identifySHA1(sha1s map[string]string) map[string]provider.Hosted {
	found := map[string]provider.Hosted{}
	for _, key := range slices.Sorted(maps.Keys(sha1s)) {
		sha1Hex := sha1s[key]
		for _, v := range p.Files {
			if v.File.Sha1 != sha1Hex {
				continue
			}
			if i := slices.IndexFunc(p.Known, func(proj provider.Project) bool { return proj.ID == v.ProjectID }); i >= 0 {
				found[key] = provider.Hosted{Project: p.Known[i], Version: v}
			}
			break
		}
	}
	return found
}

func (p *Provider) host() string { return p.name + ".test" }

func (p *Provider) Hosts() []string { return []string{p.host()} }

func (p *Provider) ParseURL(u *url.URL) (provider.Ref, error) {
	if u.Hostname() != p.host() {
		return provider.Ref{}, provider.ErrNotHosted
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) == 2:
		return provider.Ref{Project: parts[1]}, nil
	case len(parts) == 4 && parts[2] == "version":
		return provider.Ref{Project: parts[1], Version: parts[3]}, nil
	}
	return provider.Ref{}, out.Errorf("usage", "%s can't read %s", p.name, u)
}

func (p *Provider) URLShapes() []string {
	return []string{"https://" + p.host() + "/<kind>/<slug>[/version/<id>]"}
}

func (p *Provider) ProjectPage(kind, slugOrID string) string {
	if kind == "" {
		kind = "project"
	}
	return "https://" + p.host() + "/" + kind + "/" + slugOrID
}

func (p *Provider) VersionsPage(kind, slug string) string {
	return p.ProjectPage(kind, slug) + "/versions"
}
