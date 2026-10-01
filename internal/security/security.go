// Package security is the table of protections shulker applies, the one place `shulker security`
// explains them from.
package security

import (
	"errors"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/out"
)

// Stance is what shulker aims for, the paragraph `shulker security` opens with.
const Stance = "Mods run with everything your account can reach, and a linked instance updates itself on every launch. Shulker can't tell a good mod from a bad one, but it shouldn't be the easiest way to put one on your machine: every file it installs comes from where the lock says, arrives unchanged, and lands only where it belongs."

// Nudge ends every security warning and error, pointing at `shulker security`.
var Nudge = out.Nudge{Lead: "Read what shulker checks and why", Command: "shulker security"}

// ID names a protection: its row in `shulker security --json`, and an error's protection field.
type ID string

const (
	Paths            ID = "paths"
	OverrideSymlinks ID = "override-symlinks"
	HTTPS            ID = "https"
	MrpackHosts      ID = "mrpack-hosts"
	Provenance       ID = "provenance"
	CacheHash        ID = "cache-hash"
	ManifestJVMArgs  ID = "manifest-jvm-args"
	ReleaseAge       ID = "release-age"
	Takedowns        ID = "takedowns"
	PlacedJars       ID = "placed-jars"
	SyncReview       ID = "sync-review"
)

// Refusal marks e as protection id refusing something: it names the protection for --json and ends
// with Nudge.
func Refusal(id ID, e *out.Error) *out.Error {
	e.Protection = string(id)
	e.Nudge = Nudge
	return e
}

// Warn is a warning from protection id: its message, its facts for --json, and nudges, which end
// with Nudge.
func Warn(id ID, message string, data any, nudges ...out.Nudge) out.SecurityWarning {
	return out.SecurityWarning{Protection: string(id), Message: message, Data: data, Nudges: append(nudges, Nudge)}
}

// Protection is one thing shulker does to keep a bad file off the player's machine.
type Protection struct {
	ID ID `json:"id"`
	// Summary is one sentence on what shulker does and what that stops.
	Summary string `json:"summary"`
	On      bool   `json:"on"`
	// Setting is the config key that controls the protection, empty when it is always on.
	Setting string `json:"setting,omitempty"`
	Value   string `json:"value,omitempty"`
	// Changes is what the setting changes, for the settings table.
	Changes string `json:"changes,omitempty"`
}

var protections = []Protection{
	{ID: Paths, On: true, Summary: "No path in a pack, lock or manifest can reach outside its folder, so a pack can't write over files elsewhere on your machine."},
	{ID: OverrideSymlinks, On: true, Summary: "Symlinks in a source's override folders are skipped, so one can't copy a file from elsewhere on your disk, like an SSH key, into an instance."},
	{ID: HTTPS, On: true, Summary: "Every download, API call and redirect uses https, and every git remote https or ssh, so nothing can be swapped on its way to you."},
	{ID: MrpackHosts, On: true, Summary: "An mrpack downloads only from the hosts Modrinth allows, so a pack can't pull its files from anywhere else."},
	{ID: Provenance, On: true, Summary: "A lock entry that names a provider has to download from that provider's own hosts, and the loader's installer and server files from where the loader and Mojang publish them, so a lock can't pass a file from anywhere else off as a mod it names or as the installer shulker runs."},
	{ID: CacheHash, On: true, Summary: "Every file placed from the cache is checked against its hash, and every jar a launch runs from the game store against the hash its version names, so a copy changed in the cache can't spread to other instances."},
	{ID: ManifestJVMArgs, On: true, Summary: "A manifest can't add its own flags to the java command line, such as -javaagent, so a source can't run code outside its mods."},
	{ID: PlacedJars, On: true, Summary: "A build checks every jar it placed in mods/ against the copy it locked, and warns when one has changed since rather than keeping it quietly, so a jar rewritten on your disk, as Fractureiser did, gets noticed."},
	{ID: SyncReview, On: true, Summary: "A sync lists the mods it adds, the files no provider published and the entries now locked from another project, and asks before applying them at a terminal, so a pack's new code doesn't reach you unseen."},
	{ID: Takedowns, On: true, Summary: "An audit, and a sync once a day, ask Modrinth and CurseForge whether they still have each locked file, so a file taken down after you locked it, as malware is, gets noticed rather than placed from the cache for good."},
}

// Days is d in whole days, as security.minReleaseAge counts them.
func Days(d time.Duration) int { return int(d / (24 * time.Hour)) }

// Protections lists every protection, in the order `shulker security` shows them, with the release
// age as security.minReleaseAge sets it.
func Protections(releaseAge time.Duration) []Protection {
	value := "off"
	if days := Days(releaseAge); days > 0 {
		value = out.Count(days, "day", "days")
	}
	return append(slices.Clone(protections), Protection{
		ID:      ReleaseAge,
		Summary: "A version published more recently than the release age is held back when shulker chooses one, so a hijacked mod's new release has time to be caught and taken down before it reaches you.",
		On:      releaseAge > 0,
		Setting: "security.minReleaseAge",
		Value:   value,
		Changes: "How many days old a version must be before add, update, lock or a floating modpack takes it; 0 turns it off",
	})
}

// Refused is err's error when a protection refused something.
func Refused(err error) (*out.Error, bool) {
	var e *out.Error
	if errors.As(err, &e) && e.Protection != "" {
		return e, true
	}
	return nil, false
}

// Warning is a refusal written as a warning, for a launch that goes ahead on the last good build:
// its message, rows and help, each after the first a row beneath it. Warn it with e.Nudge.
func Warning(e *out.Error) string {
	lines := []string{e.Message}
	for _, row := range e.Rows {
		lines = append(lines, row.Text)
	}
	if e.Help != "" {
		lines = append(lines, "help: "+out.Period(out.Sentence(e.Help)))
	}
	return strings.Join(lines, "\n")
}
