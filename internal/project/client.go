package project

import (
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

// Authored is the project a link writes when it follows nothing, with no directory yet: the
// manifest its answers describe, asking for the newest Minecraft unless one was named and the
// loader when one was chosen, over the lock holding the platform those resolved to. The display
// name is the client's, and its slug the project's name.
func Authored(platform *lock.Lock, minecraft string, l manifest.Loader, display string) *Project {
	m := &manifest.Manifest{
		Schema:    manifest.SchemaURL,
		Minecraft: OrLatest(minecraft),
		Loader:    l,
		Requires:  map[string]manifest.Require{},
		Client:    NewClient(),
	}
	if m.Minecraft == "*" {
		m.Minecraft = platform.Minecraft
	}
	m.Name, m.Client.Name = config.SlugID(display), display
	return &Project{Manifest: m, Lock: platform}
}

// NewClient is the client block a new project starts with, its options skipping the game's
// first-run screens.
func NewClient() *manifest.Client {
	return &manifest.Client{Options: map[string]any{
		"onboardAccessibility":   false,
		"skipMultiplayerWarning": true,
		"tutorialStep":           "none",
		"joinedFirstServer":      true,
	}}
}

// OrLatest is the range an unset version means, which is the newest release.
func OrLatest(minecraft string) string {
	if minecraft == "" {
		return "*"
	}
	return minecraft
}

// PlatformName is what an authored instance is called unless the player says otherwise, the way
// launchers name a new instance after what it runs.
func PlatformName(minecraft, loaderType string) string {
	if l, ok := loader.Lookup(loaderType); ok {
		return l.Title + " " + minecraft
	}
	return "Minecraft " + minecraft
}

// ClientMemory is the heap the pack author recommends for playing this project: its own
// client.memory, else that of the first modpack it requires, by key, that names one. The packs
// are those a command opened; a project whose packs are unread answers from its manifest alone.
func (p *Project) ClientMemory() string {
	if memory := p.Manifest.ClientMemory(); memory != "" {
		return memory
	}
	if p.Packs == nil {
		return ""
	}
	for _, l := range p.Packs.Loaded {
		if memory := l.Manifest.ClientMemory(); memory != "" {
			return memory
		}
	}
	return ""
}
