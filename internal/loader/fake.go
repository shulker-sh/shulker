package loader

import (
	"context"
	"encoding/json"
)

// Fake is an in-memory row for tests of what calls the seam: it answers from what it holds and
// has no server or client install. A Fake named after a real loader keeps that loader's facts.
type Fake struct {
	Name     string
	Versions []Version
	Profile  json.RawMessage
	// ProvidesJar is the URL of a jar whose metadata says what the loader provides.
	ProvidesJar string
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
	l.installerURL, l.ensureServer, l.vanillaServer, l.launchArgs = nil, nil, nil, nil
	l.InstallServerFlag, l.InstallClientFlag = "", ""
	return l
}
