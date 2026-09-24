package loader

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestQuiltVersionsCountPrereleasesUnstable(t *testing.T) {
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2": []map[string]any{
			{"loader": map[string]any{"version": "0.31.0-beta.4"}},
			{"loader": map[string]any{"version": "0.30.1"}},
		},
	})
	got, err := quilt.Versions(context.Background(), r, "26.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Version{"0.31.0-beta.4", false}) || got[1] != (Version{"0.30.1", true}) {
		t.Fatalf("versions %v", got)
	}
}

func TestQuiltProvidesJarIsOnItsMaven(t *testing.T) {
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2/0.30.1": map[string]any{"loader": map[string]any{"maven": "org.quiltmc:quilt-loader:0.30.1"}},
		"/versions/loader/26.2/0.0.0":  map[string]any{"loader": map[string]any{"maven": "nope"}},
	})
	r.URLs[QuiltMavenURL] = "https://maven.example/release"
	url, ok, err := quilt.ProvidesJar(context.Background(), r, "26.2", "0.30.1")
	if err != nil || !ok || url != "https://maven.example/release/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar" {
		t.Fatalf("url %q, %v, %v", url, ok, err)
	}
	if _, _, err := quilt.ProvidesJar(context.Background(), r, "26.2", "0.0.0"); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("bad coordinate: %v", err)
	}
}

func TestQuiltServerProfileMustBeComplete(t *testing.T) {
	r := fakeRemote(t, QuiltMetaURL, map[string]any{
		"/versions/loader/26.2/0.30.1/server/json": map[string]any{"mainClass": "a.Main"},
	})
	if _, err := newQuiltMeta(r).serverProfile(context.Background(), "26.2", "0.30.1"); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("incomplete profile: %v", err)
	}
}

func TestQuiltLaunchJarIsDeterministic(t *testing.T) {
	libs := []string{"org.quiltmc:quilt-loader:0.30.1", "net.fabricmc:sponge-mixin:0.17.3"}
	a, err := quiltLaunchJar("a.Launcher", "a.Main", libs)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := quiltLaunchJar("a.Launcher", "a.Main", []string{libs[1], libs[0]})
	if !bytes.Equal(a, b) {
		t.Fatal("library order changed the jar")
	}
	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest string
	for _, f := range zr.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			rc, _ := f.Open()
			var buf bytes.Buffer
			buf.ReadFrom(rc)
			manifest = strings.ReplaceAll(buf.String(), "\r\n ", "")
		}
	}
	if !strings.Contains(manifest, "Main-Class: a.Launcher") || !strings.Contains(manifest, "libraries/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar") {
		t.Fatalf("manifest:\n%s", manifest)
	}
}
