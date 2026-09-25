package selfupdate

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// Dev is the version a binary reports when it carries no release version: a worktree build, or a
// `go install` of a branch.
const Dev = "dev"

// Route is where a shulker binary came from, which decides who may replace or remove it.
type Route string

const (
	// Release is a published archive: the install scripts, a GitHub download, or a package manager
	// that fetches the same bytes.
	Release Route = "release"
	// GoInstall is the Go toolchain compiling a tag out of the module cache.
	GoInstall Route = "go install"
	// Source is a contributor's `go build .` inside a clone.
	Source   Route = "source"
	Homebrew Route = "homebrew"
	Scoop    Route = "scoop"
)

type routeFacts struct {
	origin, updateLead, update, uninstall string
}

var routes = map[Route]routeFacts{
	Release:   {origin: "installed from a release", updateLead: "Install it", update: "shulker self update"},
	GoInstall: {origin: "installed with go install", updateLead: "Update it with", update: "go install shulker.sh/shulker@latest"},
	Source:    {origin: "built from source", updateLead: "Rebuild it with", update: "go build ."},
	Homebrew:  {origin: "installed by Homebrew", updateLead: "Update it with", update: "brew upgrade shulker", uninstall: "brew uninstall shulker"},
	Scoop:     {origin: "installed by Scoop", updateLead: "Update it with", update: "scoop update shulker", uninstall: "scoop uninstall shulker"},
}

func (r Route) facts() routeFacts {
	if f, ok := routes[r]; ok {
		return f
	}
	// A build with no ldflag, no VCS stamp and no plain version is a source build shulker can't
	// prove is one, so it is treated as one without being named one.
	return routes[Source]
}

// Managed reports whether shulker itself may replace the binary.
func (r Route) Managed() bool { return r == Release }

// Origin says how the binary got here, completing "this shulker was …".
func (r Route) Origin() string { return r.facts().origin }

// UpdateCommand is the route's own way to a newer shulker, and UpdateLead introduces it.
func (r Route) UpdateCommand() string { return r.facts().update }

func (r Route) UpdateLead() string { return r.facts().updateLead }

// UninstallCommand is the package manager's way to remove the binary, or "" when nothing tracks
// the file and shulker removes it itself.
func (r Route) UninstallCommand() string { return r.facts().uninstall }

// Build is what a shulker binary knows about itself.
type Build struct {
	// Version is a release version, or Dev.
	Version  string
	Commit   string
	Modified bool
	// Built is when the binary was built, RFC 3339, or "" when nothing records it.
	Built string
	// Route is "" when the binary answers to none of them.
	Route Route
}

// Origin says how the binary got here, completing "this shulker was …", with the commit a source
// build sits on.
func (b Build) Origin() string {
	if b.Commit == "" {
		return b.Route.Origin()
	}
	return b.Route.Origin() + " at " + b.ShortCommit()
}

func (b Build) ShortCommit() string {
	if len(b.Commit) > 7 {
		return b.Commit[:7]
	}
	return b.Commit
}

var (
	pseudoVersion = regexp.MustCompile(`-\d{14}-([0-9a-f]{12})$`)
	plainVersion  = regexp.MustCompile(`^v\d+\.\d+\.\d+([-+][0-9A-Za-z.+-]*)?$`)
)

// Describe classifies a binary from the version and build date its ldflags carry and the build
// info the Go toolchain stamped. A release archive has the ldflags; a worktree build has the
// vcs.* settings; a `go install` of a tag has neither, and its module version is the tag.
func Describe(version, date string, info *debug.BuildInfo) Build {
	if version != "" && version != Dev {
		return Build{Version: version, Built: date, Route: Release}
	}
	b := Build{Version: Dev}
	if info == nil {
		return b
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Commit = s.Value
		case "vcs.modified":
			b.Modified = s.Value == "true"
		case "vcs.time":
			b.Built = s.Value
		}
	}
	if b.Commit != "" {
		b.Route = Source
		return b
	}
	main := info.Main.Version
	if m := pseudoVersion.FindStringSubmatch(main); m != nil {
		b.Commit = m[1]
		return b
	}
	if plainVersion.MatchString(main) {
		return Build{Version: strings.TrimPrefix(main, "v"), Route: GoInstall}
	}
	return b
}
