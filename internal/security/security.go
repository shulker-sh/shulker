// Package security is the table of protections shulker applies, the one place `shulker security`
// explains them from.
package security

import (
	"slices"

	"shulker.sh/shulker/internal/out"
)

// Stance is what shulker aims for, the paragraph `shulker security` opens with.
const Stance = "Mods run with everything your account can reach, and a linked instance updates itself on every launch. Shulker can't tell a good mod from a bad one, but it shouldn't be the easiest way to put one on your machine: every file it installs comes from where the lock says, arrives unchanged, and lands only where it belongs."

// Nudge ends every security warning and error, pointing at `shulker security`.
var Nudge = out.Nudge{Lead: "Read what shulker checks and why", Command: "shulker security"}

// Protection is one thing shulker does to keep a bad file off the player's machine.
type Protection struct {
	ID string `json:"id"`
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
	{ID: "paths", On: true, Summary: "No path in a pack, lock or manifest can reach outside its folder, so a pack can't write over files elsewhere on your machine."},
	{ID: "override-symlinks", On: true, Summary: "Symlinks in a source's override folders are skipped, so one can't copy a file from elsewhere on your disk, like an SSH key, into an instance."},
	{ID: "https", On: true, Summary: "Every download, API call and redirect uses https, and every git remote https or ssh, so nothing can be swapped on its way to you."},
	{ID: "mrpack-hosts", On: true, Summary: "An mrpack downloads only from the hosts Modrinth allows, so a pack can't pull its files from anywhere else."},
	{ID: "cache-hash", On: true, Summary: "Every file placed from the cache is checked against its hash, so a copy changed in the cache can't spread to other instances."},
	{ID: "manifest-jvm-args", On: true, Summary: "A manifest can't add its own flags to the java command line, such as -javaagent, so a source can't run code outside its mods."},
}

// Protections lists every protection, in the order `shulker security` shows them.
func Protections() []Protection { return slices.Clone(protections) }
