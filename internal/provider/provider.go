// Package provider is the interface shulker reads mod hosts through, and the rules for picking
// which of a project's versions to use.
package provider

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
)

var ErrNotFound = errors.New("project not found")

// ErrNotHosted is ParseURL's answer for a URL on another host, which keeps its other meaning.
var ErrNotHosted = errors.New("not a provider url")

// DatapackLoader is the loader a datapack version lists. Modrinth files datapacks as mods, so a
// project whose only loader is this one is a datapack.
const DatapackLoader = "datapack"

type Project struct {
	ID    string
	Slug  string
	Title string
	Side  string
	// Type is the provider's own project type: mod, modpack, resourcepack,
	// shader or datapack. Empty when the provider doesn't say.
	Type string
	// Datapack reports a project with datapack files, whatever its Type:
	// Modrinth files a mod that also ships as a datapack as a mod.
	Datapack  bool
	Downloads int64
	// Author is the project's first author, where the provider lists one.
	Author string
	// Summary is the project's one-line description.
	Summary string
	// Page is the project's page on the provider's site.
	Page string
}

type File struct {
	URL      string
	Filename string
	Sha512   string
	Sha1     string
	Size     int64
}

type Dependency struct {
	ProjectID string
	VersionID string
	Type      string
}

type Version struct {
	ID           string
	ProjectID    string
	Number       string
	Channel      string
	Published    time.Time
	File         File
	Dependencies []Dependency
	GameVersions []string
	Loaders      []string
	Page         string
	// ServerPack is the version holding a modpack version's server files, where the provider
	// pairs one with it.
	ServerPack string
}

// Hosted is a file a provider hosts: the version it is and that version's project.
type Hosted struct {
	Project Project
	Version Version
}

// Ref is what a provider URL names: a project, by slug or id, and one of its versions when the
// URL points at one. Provider is filled by Providers.ParseURL.
type Ref struct {
	Provider string
	Project  string
	// Version is a version id, a version number where the provider names versions, or a file
	// id; empty for a project page.
	Version string
}

// Provider is one mod host, such as Modrinth or CurseForge. Every adapter implements all of it;
// what a host can't do answers with nothing rather than being left out.
type Provider interface {
	Name() string
	// Title is the name as the user reads it: CurseForge for curseforge.
	Title() string
	// Available is nil when the provider can be asked, or the provider-unavailable error saying
	// why not, with help on how to set it up.
	Available() error
	// KeysBySlug reports whether a project's slug finds it as surely as its id does, so a
	// manifest may name a project by its key alone. CurseForge finds slugs through its search,
	// which misses some projects.
	KeysBySlug() bool
	// NamesVersions reports whether a version's Number is a name its author typed rather than
	// the version its file declares. CurseForge's display names are often the filename.
	NamesVersions() bool
	// NotFoundHelp is what to try when a slug lookup misses; empty when a miss is final.
	NotFoundHelp() string
	// PackTags are the loader tags the provider files versions of a pack kind under, to ask
	// Versions for; nil asks with no loader filter.
	PackTags(kind string) []string
	// Hosts are the domains the provider serves its own files from, so a download from one of
	// them, or a subdomain, is the provider's rather than a third party's.
	Hosts() []string

	// Project looks a slug or id up. kind narrows the search to one project
	// type where the provider needs it to tell projects apart; "" searches every
	// type. Whether the result's Type agrees with what was asked for is the
	// caller's to decide.
	Project(ctx context.Context, slugOrID, kind string) (*Project, error)
	// Projects finds these projects in one request, by id. A project the provider no longer has
	// is left out.
	Projects(ctx context.Context, ids []string) (map[string]Project, error)
	// Search lists the projects matching query, at most limit of them, ranked
	// the way the provider ranks a search: by relevance where it offers that,
	// by popularity where it doesn't. kind narrows the search to one project
	// type; "" spans every type the provider offers that shulker can add.
	Search(ctx context.Context, query, kind string, limit int) ([]Project, error)
	// Versions lists a project's versions for the game version and loaders. An empty game or no
	// loaders leaves that filter off.
	Versions(ctx context.Context, projectID, game string, loaders []string) ([]Version, error)
	Version(ctx context.Context, versionID string) (*Version, error)
	// ProjectVersion reads one of a project's versions by its id, or by its number where the
	// provider names versions. Whether it belongs to project is the caller's to check.
	ProjectVersion(ctx context.Context, project, version string) (*Version, error)
	// VersionsByID finds these versions in one request. One the provider doesn't have is left
	// out, and one it has nothing to download for is in unusable with the reason.
	VersionsByID(ctx context.Context, ids []string) (found map[string]Version, unusable map[string]error, err error)
	// Identify finds the versions the provider hosts these files as, hashing each the way the
	// provider indexes files, keyed as given. A file it doesn't host is left out.
	Identify(ctx context.Context, files map[string][]byte) (map[string]Hosted, error)
	// IdentifySHA1 is Identify for files known by sha1 alone, keyed as given. A provider that
	// doesn't index files by sha1 finds none.
	IdentifySHA1(ctx context.Context, sha1s map[string]string) (map[string]Hosted, error)

	// ParseURL reads what a URL on the provider's own hosts names. It is ErrNotHosted for a URL
	// on another host, and a usage error for a shape on its hosts it doesn't read.
	ParseURL(u *url.URL) (Ref, error)
	// URLShapes are the URL shapes ParseURL reads, as the usage error lists them.
	URLShapes() []string
	// ProjectPage is the project's page. kind may be empty for an id.
	ProjectPage(kind, slugOrID string) string
	// VersionsPage lists the project's versions.
	VersionsPage(kind, slug string) string
}

// Providers is every provider shulker knows, by name, available or not.
type Providers map[string]Provider

// Get is the named provider when it can be asked, or why it can't be.
func (ps Providers) Get(name string) (Provider, error) {
	p, ok := ps[name]
	if !ok {
		return nil, out.Errorf("provider-unavailable", "%s is not a known provider", name)
	}
	if err := p.Available(); err != nil {
		return nil, err
	}
	return p, nil
}

// Title is the named provider's title, or the name for one shulker doesn't know.
func (ps Providers) Title(name string) string {
	if p, ok := ps[name]; ok {
		return p.Title()
	}
	return name
}

// ParseURL reads a provider URL. ok is false for anything else, which keeps its other meaning; a
// URL on a provider's host in a shape it doesn't read is a usage error listing every shape.
func (ps Providers) ParseURL(arg string) (Ref, bool, error) {
	if !strings.HasPrefix(arg, "https://") && !strings.HasPrefix(arg, "http://") {
		return Ref{}, false, nil
	}
	u, err := url.Parse(arg)
	if err != nil {
		return Ref{}, false, nil
	}
	for _, name := range slices.Sorted(maps.Keys(ps)) {
		ref, err := ps[name].ParseURL(u)
		if errors.Is(err, ErrNotHosted) {
			continue
		}
		if err != nil {
			e := out.Errorf("usage", "shulker can't read %s", arg)
			e.Items = ps.URLShapes()
			return Ref{}, false, e
		}
		ref.Provider = name
		return ref, true, nil
	}
	return Ref{}, false, nil
}

// URLShapes are every provider's URL shapes, in name order.
func (ps Providers) URLShapes() []string {
	var shapes []string
	for _, name := range slices.Sorted(maps.Keys(ps)) {
		shapes = append(shapes, ps[name].URLShapes()...)
	}
	return shapes
}

var channelRank = map[string]int{"release": 0, "beta": 1, "alpha": 2}

// ChannelAllows reports whether a version on the actual channel is stable enough for a manifest
// that accepts the accepted one. An empty accepted channel means release.
func ChannelAllows(accepted, actual string) bool {
	if accepted == "" {
		accepted = "release"
	}
	return channelRank[actual] <= channelRank[accepted]
}

// Newest picks the newest version the channel allows. When that is another
// loader's build of a release that also ships a build for loaderName, it
// picks that build instead.
func Newest(versions []Version, channel, loaderName string) (Version, bool) {
	best, found := newest(versions, channel, func(Version) bool { return true })
	if !found || slices.Contains(best.Loaders, loaderName) {
		return best, found
	}
	release := releaseKey(best.Number)
	own, ok := newest(versions, channel, func(v Version) bool {
		return slices.Contains(v.Loaders, loaderName) && releaseKey(v.Number) == release
	})
	if ok {
		return own, true
	}
	return best, true
}

// NewestBy is Newest among the versions published by cutoff, and the newest version it skipped for
// being published later, when it skipped one. ok is false when none was published by cutoff.
func NewestBy(versions []Version, channel, loaderName string, cutoff time.Time) (took Version, skipped *Version, ok bool) {
	newest, found := Newest(versions, channel, loaderName)
	if !found || !newest.Published.After(cutoff) {
		return newest, nil, found
	}
	old := slices.DeleteFunc(slices.Clone(versions), func(v Version) bool { return v.Published.After(cutoff) })
	took, ok = Newest(old, channel, loaderName)
	return took, &newest, ok
}

func newest(versions []Version, channel string, keep func(Version) bool) (Version, bool) {
	var best Version
	found := false
	for _, v := range versions {
		if !ChannelAllows(channel, v.Channel) || !keep(v) {
			continue
		}
		if !found || v.Published.After(best.Published) {
			best, found = v, true
		}
	}
	return best, found
}

func releaseKey(number string) string {
	fields := strings.FieldsFunc(strings.ToLower(number), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	fields = slices.DeleteFunc(fields, func(f string) bool {
		_, isLoader := loader.Lookup(f)
		return isLoader
	})
	return strings.Join(fields, ".")
}

// Serves reports whether host is one of p's own hosts or a subdomain of one.
func Serves(p Provider, host string) bool {
	return slices.ContainsFunc(p.Hosts(), func(domain string) bool {
		return host == domain || strings.HasSuffix(host, "."+domain)
	})
}
