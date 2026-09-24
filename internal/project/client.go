package project

import "shulker.sh/shulker/internal/manifest"

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
