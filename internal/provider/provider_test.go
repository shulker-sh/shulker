package provider

import (
	"testing"
	"time"
)

func TestNewestPrefersTheLoadersOwnBuild(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name     string
		versions []Version
		want     string
	}{
		{"fabric build of the same release", []Version{
			{ID: "q", Number: "1.2.0+quilt", Channel: "release", Published: day(1), Loaders: []string{"quilt"}},
			{ID: "f", Number: "1.2.0+fabric", Channel: "release", Published: day(2), Loaders: []string{"fabric"}},
		}, "q"},
		{"curseforge display names", []Version{
			{ID: "q", Number: "[Quilt] Shiny 1.2.0", Channel: "release", Published: day(1), Loaders: []string{"quilt"}},
			{ID: "f", Number: "[Fabric] Shiny 1.2.0", Channel: "release", Published: day(2), Loaders: []string{"fabric"}},
		}, "q"},
		{"newer fabric release wins", []Version{
			{ID: "q", Number: "1.1.0+quilt", Channel: "release", Published: day(1), Loaders: []string{"quilt"}},
			{ID: "f", Number: "1.2.0+fabric", Channel: "release", Published: day(2), Loaders: []string{"fabric"}},
		}, "f"},
		{"one version tagged both", []Version{
			{ID: "b", Number: "1.2.0", Channel: "release", Published: day(1), Loaders: []string{"fabric", "quilt"}},
			{ID: "f", Number: "1.2.0 fabric", Channel: "release", Published: day(2), Loaders: []string{"fabric"}},
		}, "b"},
		{"quilt build outside the channel", []Version{
			{ID: "q", Number: "1.2.0-quilt", Channel: "beta", Published: day(1), Loaders: []string{"quilt"}},
			{ID: "f", Number: "1.2.0-fabric", Channel: "release", Published: day(2), Loaders: []string{"fabric"}},
		}, "f"},
		{"fabric only", []Version{
			{ID: "f1", Number: "1.1.0", Channel: "release", Published: day(1), Loaders: []string{"fabric"}},
			{ID: "f2", Number: "1.2.0", Channel: "release", Published: day(2), Loaders: []string{"fabric"}},
		}, "f2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, ok := Newest(c.versions, "release", "quilt")
			if !ok || v.ID != c.want {
				t.Errorf("Newest = %q, %v; want %q", v.ID, ok, c.want)
			}
		})
	}
	if _, ok := Newest(nil, "release", "quilt"); ok {
		t.Error("Newest(nil) found a version")
	}
}
