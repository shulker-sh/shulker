package resolve

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func TestSplitLocalFilesKeepsTheOrderGiven(t *testing.T) {
	m := &manifest.Manifest{Requires: map[string]manifest.Require{
		"sodium":  {},
		"private": {File: "files/private.jar"},
		"base":    {File: "files/base.mrpack", Type: manifest.TypeModpack},
	}}

	local, rest := SplitLocalFiles(m, []string{"base", "private", "sodium", "missing"})

	if want := []string{"private"}; !slices.Equal(local, want) {
		t.Fatalf("local = %q, want %q", local, want)
	}
	if want := []string{"base", "sodium", "missing"}; !slices.Equal(rest, want) {
		t.Fatalf("rest = %q, want %q", rest, want)
	}
}
