package loader

import (
	"context"
	"encoding/json"

	"shulker.sh/shulker/internal/lock"
)

// Fake is an in-memory row for tests of what calls the seam: it answers from what it holds and
// has no server or client install unless given one. A Fake named after a real loader keeps that
// loader's facts.
type Fake struct {
	Name     string
	Versions []Version
	Profile  json.RawMessage
	// ProvidesJar is the URL of a jar whose metadata says what the loader provides.
	ProvidesJar string
	// Installer is the URL of the loader's own installer jar; set, a client is set up by running it
	// rather than from a profile.
	Installer string
	// EnsureServer stands in for the loader's server install, caching and locking what its server
	// starts from; set, the row keeps the real loader's server placement and launch around it.
	EnsureServer func(ctx context.Context, r *Remote, lk *lock.Lock) (ServerResult, error)
}

// Row is the in-memory row the Fake answers as.
func (f Fake) Row() Loader {
	l, _ := Lookup(f.Name)
	l.Name = f.Name
	l.versions = func(context.Context, *Remote, string) ([]Version, error) { return f.Versions, nil }
	l.profile = nil
	if f.Profile != nil {
		l.profile = func(context.Context, *Remote, string, string) (json.RawMessage, error) { return f.Profile, nil }
	}
	l.providesJar = nil
	if f.ProvidesJar != "" {
		l.providesJar = func(context.Context, *Remote, string, string) (string, error) { return f.ProvidesJar, nil }
	}
	l.installerURL, l.ensureServer = nil, nil
	if f.Installer != "" {
		l.installerURL = func(*Remote, string, string) string { return f.Installer }
		if l.InstallClientFlag == "" {
			l.InstallClientFlag = "--install-client"
		}
	} else {
		l.InstallClientFlag = ""
	}
	if f.EnsureServer != nil {
		l.ensureServer = func(ctx context.Context, _ Loader, r *Remote, lk *lock.Lock) (ServerResult, error) {
			return f.EnsureServer(ctx, r, lk)
		}
	} else {
		l.vanillaServer, l.launchArgs, l.InstallServerFlag = nil, nil, ""
	}
	return l
}
