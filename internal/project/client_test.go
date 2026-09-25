package project

import (
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
)

func TestClientMemoryFallsBackToTheFirstPackThatNamesOne(t *testing.T) {
	own := &Project{Manifest: &manifest.Manifest{Client: &manifest.Client{Memory: "8G"}}}
	packs := &OpenedPacks{Loaded: []*modpack.Loaded{
		{Name: "a", Manifest: &manifest.Manifest{}},
		{Name: "b", Manifest: &manifest.Manifest{Client: &manifest.Client{Memory: "6G"}}},
		{Name: "c", Manifest: &manifest.Manifest{Client: &manifest.Client{Memory: "2G"}}},
	}}
	if got := own.ClientMemory(); got != "8G" {
		t.Fatalf("the project's own client.memory wins: %q", got)
	}
	p := &Project{Manifest: &manifest.Manifest{}, Packs: packs}
	if got := p.ClientMemory(); got != "6G" {
		t.Fatalf("the first pack naming one: %q", got)
	}
	p.Packs = nil
	if got := p.ClientMemory(); got != "" {
		t.Fatalf("no packs read and none of its own: %q", got)
	}
}
