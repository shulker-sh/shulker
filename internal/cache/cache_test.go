package cache

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fetch"
)

func sha1Of(content string) string {
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestAPutObjectIsFoundByItsSha1(t *testing.T) {
	c := newCache(t)
	sha, err := c.Put(strings.NewReader("a mod"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := c.BySha1(sha1Of("a mod")); !ok || got != sha {
		t.Fatalf("BySha1 = %q, %v; want %q", got, ok, sha)
	}
	if _, ok := c.BySha1(sha1Of("something else")); ok {
		t.Fatal("a sha1 the cache never saw was found")
	}
	if _, ok := c.BySha1(""); ok {
		t.Fatal("an empty sha1 was found")
	}
	if _, ok := c.BySha1("../../objects/" + sha[:2] + "/" + sha); ok {
		t.Fatal("a sha1 that isn't one was read as a path")
	}
}

func TestAFetchedObjectIsFoundByItsSha1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("a curseforge jar"))
	}))
	defer srv.Close()
	c := newCache(t)
	sha, err := c.Fetch(context.Background(), fetch.New("test"), srv.URL+"/a.jar")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := c.BySha1(sha1Of("a curseforge jar")); !ok || got != sha {
		t.Fatalf("BySha1 = %q, %v; want %q", got, ok, sha)
	}
}

func TestAnObjectGoneIsNotFoundByItsSha1(t *testing.T) {
	c := newCache(t)
	sha, err := c.Put(strings.NewReader("a mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(c.Object(sha)); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.BySha1(sha1Of("a mod")); ok {
		t.Fatal("an object removed by hand was still found")
	}
}

func TestPruneDropsTheIndexEntriesOfAnObjectItRemoves(t *testing.T) {
	c := newCache(t)
	sha, err := c.PutManual(strings.NewReader("nothing references me"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prune(nil, PruneOptions{Manual: true}); err != nil {
		t.Fatal(err)
	}
	if exists(c.sha1Entry(sha1Of("nothing references me"))) || exists(c.manualMarker(sha)) {
		t.Fatal("the pruned object's index entries are still there")
	}
}

func TestAManualPutIsMarkedAndFoundByItsSha1(t *testing.T) {
	c := newCache(t)
	sha, err := c.PutManual(strings.NewReader("a jar downloaded by hand"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsManual(sha) {
		t.Fatal("a manual put isn't marked manual")
	}
	if got, ok := c.BySha1(sha1Of("a jar downloaded by hand")); !ok || got != sha {
		t.Fatalf("BySha1 = %q, %v; want %q", got, ok, sha)
	}
	plain, err := c.Put(strings.NewReader("a mod"))
	if err != nil {
		t.Fatal(err)
	}
	if c.IsManual(plain) {
		t.Fatal("a plain put is marked manual")
	}
}

func TestPruneKeepsManualDownloadsUnlessAsked(t *testing.T) {
	c := newCache(t)
	sha, err := c.PutManual(strings.NewReader("a jar downloaded by hand"))
	if err != nil {
		t.Fatal(err)
	}
	if u, err := c.Usage(); err != nil || u.Manual != 1 {
		t.Fatalf("usage counts the manual download: %+v %v", u, err)
	}
	if p, err := c.Prune(nil, PruneOptions{}); err != nil || p.Files != 0 || !c.Has(sha) || !c.IsManual(sha) {
		t.Fatalf("a manual download nothing references is kept: %+v %v", p, err)
	}
	if p, err := c.Prune(nil, PruneOptions{DryRun: true, Manual: true}); err != nil || p.Files != 1 || !c.Has(sha) {
		t.Fatalf("a dry run with manual counts it and keeps it: %+v %v", p, err)
	}
	if p, err := c.Prune(nil, PruneOptions{Manual: true}); err != nil || p.Files != 1 || c.Has(sha) || c.IsManual(sha) {
		t.Fatalf("with manual it goes, marker and all: %+v %v", p, err)
	}
	if u, err := c.Usage(); err != nil || u.Manual != 0 {
		t.Fatalf("usage counts no manual download once it is gone: %+v %v", u, err)
	}
}

func TestADamagedSha1EntryFindsNothing(t *testing.T) {
	c := newCache(t)
	for _, content := range []string{"", "a", "not a sha512"} {
		writeFile(t, c.sha1Entry(sha1Of("a mod")), content)
		if _, ok := c.BySha1(sha1Of("a mod")); ok {
			t.Fatalf("an entry holding %q was found", content)
		}
	}
}

func TestMarkManualMarksAPlainObject(t *testing.T) {
	c := newCache(t)
	sha, err := c.Put(strings.NewReader("a jar downloaded by hand"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MarkManual(sha); err != nil || !c.IsManual(sha) {
		t.Fatalf("the object is marked manual: %v", err)
	}
	if got, ok := c.BySha1(sha1Of("a jar downloaded by hand")); !ok || got != sha {
		t.Fatal("a marked object is found by its sha1")
	}
}

func TestAFetchWhoseSha1MismatchesLeavesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("a curseforge jar"))
	}))
	defer srv.Close()
	c := newCache(t)
	if _, err := c.FetchChecked(context.Background(), fetch.New("test"), srv.URL+"/a.jar", sha1Of("another jar")); err == nil {
		t.Fatal("a download whose sha1 mismatches was taken")
	}
	if _, ok := c.BySha1(sha1Of("a curseforge jar")); ok {
		t.Fatal("a download whose sha1 mismatches reached the cache")
	}
	sha, err := c.FetchChecked(context.Background(), fetch.New("test"), srv.URL+"/a.jar", sha1Of("a curseforge jar"))
	if err != nil || !c.Has(sha) {
		t.Fatalf("a download whose sha1 matches is cached: %v", err)
	}
}
