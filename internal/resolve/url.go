package resolve

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// ProviderURL is what a Modrinth or CurseForge page, file or CDN link names.
type ProviderURL struct {
	Provider string
	// Project is a slug or a project id.
	Project string
	// Version is a Modrinth version id or number, or a CurseForge file id; empty for a project page.
	Version string
}

var (
	modrinthHosts    = []string{"modrinth.com", "www.modrinth.com"}
	curseforgeHosts  = []string{"curseforge.com", "www.curseforge.com", "legacy.curseforge.com"}
	modrinthSections = []string{"mod", "project", "plugin", "resourcepack", "shader", "datapack", "modpack"}
	// curseforgeSections are the curseforge.com/minecraft/<section> of each kind shulker can add.
	curseforgeSections = map[string]string{
		manifest.TypeMod:          "mc-mods",
		manifest.TypeModpack:      "modpacks",
		manifest.TypeResourcePack: "texture-packs",
		manifest.TypeShader:       "shaders",
		manifest.TypeDatapack:     "data-packs",
	}
)

var urlShapes = []string{
	"https://modrinth.com/<mod|resourcepack|shader|datapack|modpack>/<slug or id>[/version/<id or number>]",
	"https://cdn.modrinth.com/data/<project>/versions/<version>/<file>",
	"https://www.curseforge.com/minecraft/<mc-mods|texture-packs|shaders|data-packs|modpacks>/<slug>[/files/<file id>]",
	"https://www.curseforge.com/projects/<project id>",
}

// ParseURL reads a Modrinth or CurseForge URL. ok is false for anything else, which keeps its other
// meaning; a URL on one of their hosts in a shape shulker doesn't read is a usage error.
func ParseURL(arg string) (ProviderURL, bool, error) {
	if !strings.HasPrefix(arg, "https://") && !strings.HasPrefix(arg, "http://") {
		return ProviderURL{}, false, nil
	}
	u, err := url.Parse(arg)
	if err != nil {
		return ProviderURL{}, false, nil
	}
	host := strings.ToLower(u.Hostname())
	var parts []string
	for part := range strings.SplitSeq(u.Path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	var parsed ProviderURL
	var read bool
	switch {
	case slices.Contains(modrinthHosts, host):
		parsed, read = modrinthURL(parts)
	case host == "cdn.modrinth.com":
		if len(parts) == 5 && parts[0] == "data" && parts[2] == "versions" {
			parsed, read = ProviderURL{Provider: "modrinth", Project: parts[1], Version: parts[3]}, true
		}
	case slices.Contains(curseforgeHosts, host):
		parsed, read = curseforgeURL(parts)
	default:
		return ProviderURL{}, false, nil
	}
	if !read {
		e := out.Errorf("usage", "shulker can't read %s", arg)
		e.Items = urlShapes
		return ProviderURL{}, false, e
	}
	return parsed, true, nil
}

func modrinthURL(parts []string) (ProviderURL, bool) {
	if len(parts) < 2 || !slices.Contains(modrinthSections, parts[0]) {
		return ProviderURL{}, false
	}
	switch {
	case len(parts) == 2:
		return ProviderURL{Provider: "modrinth", Project: parts[1]}, true
	case len(parts) == 4 && parts[2] == "version":
		return ProviderURL{Provider: "modrinth", Project: parts[1], Version: parts[3]}, true
	}
	return ProviderURL{}, false
}

func curseforgeURL(parts []string) (ProviderURL, bool) {
	numeric := func(s string) bool {
		_, err := strconv.Atoi(s)
		return err == nil
	}
	if len(parts) == 2 && parts[0] == "projects" && numeric(parts[1]) {
		return ProviderURL{Provider: "curseforge", Project: parts[1]}, true
	}
	if len(parts) < 3 || parts[0] != "minecraft" || !slices.Contains(slices.Collect(maps.Values(curseforgeSections)), parts[1]) {
		return ProviderURL{}, false
	}
	switch {
	case len(parts) == 3:
		return ProviderURL{Provider: "curseforge", Project: parts[2]}, true
	case len(parts) == 5 && (parts[3] == "files" || parts[3] == "download") && numeric(parts[4]):
		return ProviderURL{Provider: "curseforge", Project: parts[2], Version: parts[4]}, true
	}
	return ProviderURL{}, false
}

// projectVersioner reads a version through its project, which takes a version number as well as
// an id.
type projectVersioner interface {
	ProjectVersion(ctx context.Context, project, version string) (*provider.Version, error)
}

// target is the project a URL names and the version it pins: a CurseForge file's project by its id,
// since the slug in the URL may be one CurseForge's search can't find.
func (r *Resolver) target(ctx context.Context, u ProviderURL) (project, version string, err error) {
	if u.Version == "" {
		return u.Project, "", nil
	}
	p, err := r.provider(u.Provider)
	if err != nil {
		return "", "", err
	}
	var v *provider.Version
	if pv, ok := p.(projectVersioner); ok {
		v, err = pv.ProjectVersion(ctx, u.Project, u.Version)
	} else {
		v, err = p.Version(ctx, u.Version)
	}
	if errors.Is(err, provider.ErrNotFound) {
		return "", "", out.Errorf("version-not-found", "%s has no version %s for %s", u.Provider, u.Version, u.Project)
	}
	if err != nil {
		return "", "", err
	}
	return v.ProjectID, v.ID, nil
}

// FromURL is the slug and options that add what u names: its provider, and its version as the pin.
// Flags that name something else are refused.
func (r *Resolver) FromURL(ctx context.Context, u ProviderURL, opts AddOptions) (string, AddOptions, error) {
	if opts.Provider != "" && opts.Provider != u.Provider {
		return "", opts, out.Errorf("usage", "--provider %s disagrees with the %s URL", opts.Provider, u.Provider)
	}
	project, version, err := r.target(ctx, u)
	if err != nil {
		return "", opts, err
	}
	if version != "" && opts.Pin != "" && opts.Pin != version && opts.Pin != u.Version {
		return "", opts, out.Errorf("usage", "--pin %s disagrees with the URL's version %s", opts.Pin, u.Version)
	}
	opts.Provider = u.Provider
	if version != "" {
		opts.Pin = version
	}
	return project, opts, nil
}

// PinURL pins key to the version u names, which must be of the project key is locked from.
func (r *Resolver) PinURL(ctx context.Context, key string, u ProviderURL) (string, error) {
	if u.Version == "" {
		return "", out.Errorf("usage", "the URL names a project, not a version to pin %s to", key)
	}
	lockedProvider, lockedProject, ok := r.lockedSource(key)
	if !ok {
		if err := r.refuseLocalPin(key); err != nil {
			return "", err
		}
		if _, err := r.directTargets([]string{key}); err != nil {
			return "", err
		}
		return "", out.Errorf("usage", "%s is not locked from a provider", key)
	}
	if u.Provider != lockedProvider {
		return "", switchProject(out.Errorf("usage", "%s is locked from %s, not %s", key, lockedProvider, u.Provider), key)
	}
	project, version, err := r.target(ctx, u)
	if err != nil {
		return "", err
	}
	if project != lockedProject {
		return "", switchProject(out.Errorf("usage", "the URL's version belongs to project %s, not %s's project %s", project, key, lockedProject), key)
	}
	return r.Pin(ctx, key, version)
}

func switchProject(e *out.Error, key string) *out.Error {
	e.Help = fmt.Sprintf("to switch it, `shulker remove %s` and add the URL", key)
	return e
}

func (r *Resolver) lockedSource(key string) (providerName, project string, ok bool) {
	if m, found := r.Lock.Mods[key]; found && m.Provider != "" {
		return m.Provider, m.Project.String(), true
	}
	if m, found := r.Lock.Modpacks[key]; found && m.Provider != "" {
		return m.Provider, m.Project.String(), true
	}
	return "", "", false
}
