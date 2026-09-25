package modpack

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

var archiveBytes = []byte("PK\x03\x04 a hosted modpack archive")

func hexSHA512(b []byte) string {
	sum := sha512.Sum512(b)
	return hex.EncodeToString(sum[:])
}

func newHostedStore(t *testing.T) (*Store, *[]string) {
	t.Helper()
	var log []string
	s := &Store{
		Cache:      &cache.Cache{Dir: t.TempDir()},
		ProjectDir: t.TempDir(),
		Fetch:      fetch.New("test"),
		Log:        func(format string, args ...any) { log = append(log, format) },
		Warn:       func(string, ...any) {},
	}
	return s, &log
}

func TestFetchHostedCachesTheArchiveFromItsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pack.mrpack" {
			http.NotFound(w, r)
			return
		}
		w.Write(archiveBytes)
	}))
	t.Cleanup(srv.Close)
	s, log := newHostedStore(t)
	url := srv.URL + "/pack.mrpack"
	l := &Loaded{Name: "fo", Kind: Hosted, Pin: lock.Modpack{URL: &url, Sha512: hexSHA512(archiveBytes), VersionNumber: "1.0"}}
	if err := s.fetchHosted(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	if !s.Cache.Has(l.Pin.Sha512) {
		t.Fatal("the archive is not in the cache")
	}
	if len(*log) != 1 || !strings.Contains((*log)[0], "fetching modpack") {
		t.Errorf("log = %q, want one fetching line", *log)
	}
}

func TestFetchHostedRefusesADownloadWithTheWrongHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("other bytes")) }))
	t.Cleanup(srv.Close)
	s, _ := newHostedStore(t)
	url := srv.URL + "/pack.mrpack"
	l := &Loaded{Name: "fo", Kind: Hosted, Pin: lock.Modpack{URL: &url, Sha512: hexSHA512(archiveBytes)}}
	err := s.fetchHosted(context.Background(), l)
	if err == nil || s.Cache.Has(l.Pin.Sha512) {
		t.Fatalf("err = %v, cached = %v; want a failure and nothing cached", err, s.Cache.Has(l.Pin.Sha512))
	}
}

func TestFetchHostedTakesAManualDownloadByItsHash(t *testing.T) {
	s, log := newHostedStore(t)
	dir := filepath.Join(s.ProjectDir, DownloadsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"other.mrpack": []byte("not it"), ".DS_Store": archiveBytes, "renamed.zip": archiveBytes} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := &Loaded{Name: "fo", Kind: Hosted, Pin: lock.Modpack{Sha512: hexSHA512(archiveBytes), Filename: "fo-1.0.mrpack"}}
	if err := s.fetchHosted(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	if !s.Cache.Has(l.Pin.Sha512) {
		t.Fatal("the dropped archive is not in the cache")
	}
	if len(*log) != 0 {
		t.Errorf("log = %q, want nothing for a file already on disk", *log)
	}
}

func TestFetchHostedAsksForAManualDownload(t *testing.T) {
	s, _ := newHostedStore(t)
	l := &Loaded{Name: "fo", Kind: Hosted, Pin: lock.Modpack{Sha512: hexSHA512(archiveBytes), Filename: "fo-1.0.mrpack", VersionNumber: "1.0", Page: "https://example.test/fo"}}
	err := s.fetchHosted(context.Background(), l)
	e := out.AsError(err)
	if e == nil || e.Code != "missing-files" {
		t.Fatalf("err = %v, want missing-files", err)
	}
	if len(e.Items) != 1 || !strings.Contains(e.Items[0], "fo-1.0.mrpack") || !strings.Contains(e.Items[0], "https://example.test/fo") || !strings.Contains(e.Items[0], DownloadsDir+"/") {
		t.Errorf("items = %q, want the file, its page and the downloads folder", e.Items)
	}
}

func TestOpenHostedNeedsTheLockedHash(t *testing.T) {
	s, _ := newHostedStore(t)
	err := s.openHosted(context.Background(), &Loaded{Name: "fo", Kind: Hosted})
	if out.CodeOf(err) != "modpack-unlocked" {
		t.Fatalf("err = %v, want modpack-unlocked", err)
	}
}

func TestStatusOfAFileModpackFollowsItsArchive(t *testing.T) {
	s, _ := newHostedStore(t)
	path := filepath.Join(s.ProjectDir, "fo.mrpack")
	if err := os.WriteFile(path, archiveBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	p := manifest.Require{File: "fo.mrpack"}
	pinned := lock.Modpack{File: "fo.mrpack", Sha512: hexSHA512(archiveBytes), Size: int64(len(archiveBytes))}
	want := func(state string) {
		t.Helper()
		st, err := s.Status("fo", p, pinned, true)
		if err != nil {
			t.Fatal(err)
		}
		if st.Kind != File || st.Source != "fo.mrpack" || st.State != state {
			t.Errorf("status = %+v, want file fo.mrpack %s", st, state)
		}
	}
	want("ok")
	if err := os.WriteFile(path, []byte(strings.ToUpper(string(archiveBytes))), 0o644); err != nil {
		t.Fatal(err)
	}
	want("changed")
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	want("changed")
	os.Remove(path)
	want("missing")
	st, err := s.Status("fo", p, pinned, false)
	if err != nil || st.State != "unlocked" {
		t.Errorf("unlocked status = %+v, %v", st, err)
	}
}

func TestCompatibleNamesLoadersAndRanges(t *testing.T) {
	fabric := lock.Loader{Type: "fabric", Version: "0.17.3"}
	cases := []struct {
		name string
		l    *Loaded
		code string
		msg  string
	}{
		{"no loader vs fabric", &Loaded{Name: "fo", Manifest: &manifest.Manifest{Minecraft: "26.2"}}, "modpack-mismatch", "modpack fo uses no loader; this project uses fabric"},
		{"unreadable minecraft range", &Loaded{Name: "fo", Manifest: &manifest.Manifest{Minecraft: "not a range"}}, "manifest-invalid", "modpack fo has a minecraft range shulker can't read"},
		{"unreadable loader range", &Loaded{Name: "fo", Manifest: &manifest.Manifest{Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric", Version: "not a range"}}}, "manifest-invalid", "modpack fo has a loader range shulker can't read"},
		{"locked for no loader", &Loaded{Name: "fo", UsesLock: true, Lock: &lock.Lock{Minecraft: "26.2"}}, "modpack-mismatch", "locked modpack fo is built for no loader; this project locked fabric 0.17.3"},
		{"locked for another version", &Loaded{Name: "fo", UsesLock: true, Lock: &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "fabric", Version: "0.17.0"}}}, "modpack-mismatch", "locked modpack fo is built for fabric 0.17.0; this project locked fabric 0.17.3"},
		{"locked and matching", &Loaded{Name: "fo", UsesLock: true, Lock: &lock.Lock{Minecraft: "26.2", Loader: fabric}}, "", ""},
	}
	for _, c := range cases {
		err := Compatible(c.l, "26.2", fabric)
		if out.CodeOf(err) != c.code {
			t.Errorf("%s: err = %v, want code %q", c.name, err, c.code)
			continue
		}
		if err == nil {
			continue
		}
		if e := out.AsError(err); e.Message != c.msg {
			t.Errorf("%s: message %q, want %q", c.name, e.Message, c.msg)
		}
	}
}
