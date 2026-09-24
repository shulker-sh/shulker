package project

import (
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
)

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
