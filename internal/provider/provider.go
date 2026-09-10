package provider

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("project not found")

type Project struct {
	ID    string
	Slug  string
	Title string
	Side  string
}

type File struct {
	URL      string
	Filename string
	Sha512   string
	Sha1     string
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

type Provider interface {
	Name() string
	Project(ctx context.Context, slugOrID string) (*Project, error)
	Versions(ctx context.Context, projectID, game, loader string) ([]Version, error)
	Version(ctx context.Context, versionID string) (*Version, error)
}

var channelRank = map[string]int{"release": 0, "beta": 1, "alpha": 2}

func ChannelAllows(accepted, actual string) bool {
	if accepted == "" {
		accepted = "release"
	}
	return channelRank[actual] <= channelRank[accepted]
}

func Newest(versions []Version, channel string) (Version, bool) {
	var best Version
	found := false
	for _, v := range versions {
		if !ChannelAllows(channel, v.Channel) {
			continue
		}
		if !found || v.Published.After(best.Published) {
			best, found = v, true
		}
	}
	return best, found
}
