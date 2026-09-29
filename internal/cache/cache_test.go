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
	if _, err := c.Prune(nil, false); err != nil {
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
