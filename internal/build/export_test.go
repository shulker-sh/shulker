package build

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

func jarWith(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(data))
	}
	zw.Close()
	return buf.Bytes()
}

func TestIdentifyOnMatchesByFingerprintThenLookalike(t *testing.T) {
	locked := jarWith(t, map[string]string{"fabric.mod.json": `{"id": "sodium"}`})
	rezipped := jarWith(t, map[string]string{"fabric.mod.json": `{"id": "sodium"}`})
	other := jarWith(t, map[string]string{"fabric.mod.json": `{"id": "lithium"}`})
	sum := sha1.Sum(other)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(rezipped) }))
	defer server.Close()
	modrinth := fake.New("modrinth")
	modrinth.Known = []provider.Project{{ID: "AANobbMI", Slug: "sodium", Type: manifest.TypeMod}}
	cf := fake.New("curseforge")
	cf.Known = []provider.Project{{ID: "394468", Slug: "sodium", Type: manifest.TypeMod}, {ID: "360438", Slug: "lithium", Type: manifest.TypeMod}}
	cf.Files = []provider.Version{
		{ID: "5000001", ProjectID: "394468", GameVersions: []string{"26.2"}, Loaders: []string{"fabric"}, File: provider.File{Filename: "sodium.jar", Size: int64(len(locked)), URL: server.URL + "/sodium.jar"}},
		{ID: "5000002", ProjectID: "360438", GameVersions: []string{"26.2"}, Loaders: []string{"fabric"}, File: provider.File{Filename: "lithium.jar", Sha1: hex.EncodeToString(sum[:])}},
	}
	b := &Builder{Lock: &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "fabric"}}, Providers: provider.Providers{"modrinth": modrinth, "curseforge": cf}, Fetch: fetch.New("test")}
	format, _ := packarchive.Lookup("curseforge")
	entries := map[string]exportEntry{
		"sodium":  {key: "sodium", kind: manifest.TypeMod, provider: "modrinth", project: "AANobbMI", providerFilename: "sodium.jar", loaders: []string{"fabric"}},
		"lithium": {key: "lithium", kind: manifest.TypeMod, provider: "modrinth", project: "lithium", providerFilename: "lithium.jar", loaders: []string{"fabric"}},
	}
	report := &ExportReport{}
	matched, err := b.identifyOn(context.Background(), format, []string{"lithium", "sodium"}, map[string][]byte{"sodium": locked, "lithium": other}, entries, false, report)
	if err != nil {
		t.Fatal(err)
	}
	if matched["lithium"].ID != "5000002" || matched["sodium"].ID != "5000001" {
		t.Fatalf("lithium by fingerprint, sodium by lookalike: %+v", matched)
	}
	if cf.Requests["Identify"] != 1 || cf.Requests["Versions"] != 1 || len(report.Warnings) != 1 {
		t.Fatalf("one fingerprint request, one lookalike, one warning: %+v %v", cf.Requests, report.Warnings)
	}

	cf.Files[0].File.Size++
	report = &ExportReport{}
	matched, err = b.identifyOn(context.Background(), format, []string{"sodium"}, map[string][]byte{"sodium": locked}, entries, true, report)
	if err != nil || len(matched) != 0 || len(report.Warnings) != 0 {
		t.Fatalf("a lookalike of another size is no match: %+v %v %v", matched, report.Warnings, err)
	}

	cf.Unavailable = provider.ErrNotFound
	if _, err := b.identifyOn(context.Background(), format, []string{"sodium"}, map[string][]byte{"sodium": locked}, entries, false, report); err == nil {
		t.Fatal("a provider that can't be asked fails the export without --bundle")
	}
	report = &ExportReport{}
	if matched, err := b.identifyOn(context.Background(), format, []string{"sodium"}, map[string][]byte{"sodium": locked}, entries, true, report); err != nil || len(matched) != 0 || len(report.Warnings) != 1 {
		t.Fatalf("with --bundle the failure is a warning: %v %v", err, report.Warnings)
	}
}
