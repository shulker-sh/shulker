// Package provider is the interface shulker reads mod hosts through, and the rules for picking
// which of a project's versions to use.
package provider

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"

	"shulker.sh/shulker/internal/loader"
)

var ErrNotFound = errors.New("project not found")

type Project struct {
	ID    string
	Slug  string
	Title string
	Side  string
	// Type is the provider's own project type: mod, modpack, resourcepack or
	// shader. Empty when the provider doesn't say.
	Type      string
	Downloads int64
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
}

// Provider is one mod host, such as Modrinth or CurseForge.
type Provider interface {
	Name() string
	// Project looks a slug or id up. kind narrows the search to one project
	// type where the provider needs it to tell projects apart; "" searches every
	// type. Whether the result's Type agrees with what was asked for is the
	// caller's to decide.
	Project(ctx context.Context, slugOrID, kind string) (*Project, error)
	// Search lists the projects matching query, at most limit of them, ranked
	// the way the provider ranks a search: by relevance where it offers that,
	// by popularity where it doesn't. kind narrows the search to one project
	// type; "" spans every type the provider offers that shulker can add.
	Search(ctx context.Context, query, kind string, limit int) ([]Project, error)
	Versions(ctx context.Context, projectID, game string, loaders []string) ([]Version, error)
	Version(ctx context.Context, versionID string) (*Version, error)
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
