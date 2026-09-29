package takedown

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

func lockWith(mods map[string]lock.Mod) *lock.Lock {
	l := lock.New()
	for key, m := range mods {
		m.Filename, m.Side, m.RequiredBy, m.Aliases = key+".jar", "both", []string{}, lock.Aliases{}
		l.Mods[key] = m
	}
	return l
}

func statuses(r Result) map[string]Status {
	got := map[string]Status{}
	for _, f := range r.Files {
		got[f.Key] = f.Status
	}
	return got
}

func TestEachFileIsPresentGoneOrMovedFromOneRequestPerProvider(t *testing.T) {
	modrinth := fake.New("modrinth")
	modrinth.Files = []provider.Version{
		{ID: "s1", ProjectID: "sodium", File: provider.File{Sha512: "s"}},
		{ID: "x1", ProjectID: "elsewhere", File: provider.File{Sha512: "m"}},
	}
	curseforge := fake.New("curseforge")
	curseforge.Files = []provider.Version{{ID: "j1", ProjectID: "238222", File: provider.File{Sha512: "j"}}}
	l := lockWith(map[string]lock.Mod{
		"sodium":  {Provider: "modrinth", Project: "sodium", Version: "s1", Sha512: "s"},
		"lithium": {Provider: "modrinth", Project: "lithium", Version: "l1", Sha512: "l"},
		"moved":   {Provider: "modrinth", Project: "moved", Version: "m1", Sha512: "m"},
		"jei":     {Provider: "curseforge", Project: "238222", Version: "j1", Sha512: "j"},
		"local":   {File: "jars/local.jar", Sha512: "x"},
	})

	r := Check(context.Background(), provider.Providers{"modrinth": modrinth, "curseforge": curseforge}, nil, Entries(l))
	if got := statuses(r); len(got) != 4 || got["sodium"] != Present || got["lithium"] != Gone || got["moved"] != Moved || got["jei"] != Present {
		t.Fatalf("statuses: %v", got)
	}
	if moved := r.With(Moved); len(moved) != 1 || moved[0].FiledUnder != "elsewhere" {
		t.Fatalf("moved: %+v", moved)
	}
	if modrinth.Requests["Filed"] != 1 || curseforge.Requests["Filed"] != 1 || len(r.Skipped) != 0 {
		t.Fatalf("one request per provider: modrinth %d, curseforge %d, skipped %v", modrinth.Requests["Filed"], curseforge.Requests["Filed"], r.Skipped)
	}
}

func TestAProviderThatCantBeAskedLeavesItsFilesUnchecked(t *testing.T) {
	modrinth := fake.New("modrinth")
	modrinth.FiledErr = errors.New("offline")
	curseforge := fake.New("curseforge")
	curseforge.Unavailable = errors.New("no CurseForge key")
	l := lockWith(map[string]lock.Mod{
		"sodium": {Provider: "modrinth", Project: "sodium", Version: "s1", Sha512: "s"},
		"jei":    {Provider: "curseforge", Project: "238222", Version: "j1", Sha512: "j"},
	})

	r := Check(context.Background(), provider.Providers{"modrinth": modrinth, "curseforge": curseforge}, nil, Entries(l))
	if got := statuses(r); got["sodium"] != Unchecked || got["jei"] != Unchecked || len(r.With(Gone)) != 0 {
		t.Fatalf("nothing is gone when nobody was asked: %v", got)
	}
	if len(r.Skipped) != 2 || r.Skipped[0].Provider != "curseforge" || r.Skipped[1] != (Skipped{Provider: "modrinth", Reason: "offline"}) {
		t.Fatalf("skipped: %+v", r.Skipped)
	}
}

func TestAFileKnownByContentIsReadFromTheCache(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	sha512Sum := sha512.Sum512([]byte("jei"))
	jei := hex.EncodeToString(sha512Sum[:])
	path := c.Object(jei)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("jei"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum([]byte("jei"))
	curseforge := fake.New("curseforge")
	curseforge.ByContent = true
	curseforge.Files = []provider.Version{{ID: "j1", ProjectID: "238222", File: provider.File{Sha1: hex.EncodeToString(sum[:])}}}
	l := lockWith(map[string]lock.Mod{
		"jei":      {Provider: "curseforge", Project: "238222", Version: "j1", Sha512: jei},
		"uncached": {Provider: "curseforge", Project: "1", Version: "u1", Sha512: strings.Repeat("0", 128)},
	})

	r := Check(context.Background(), provider.Providers{"curseforge": curseforge}, c, Entries(l))
	if got := statuses(r); got["jei"] != Present || got["uncached"] != Unchecked {
		t.Fatalf("the cached file is found by its bytes, the other left unchecked: %v", got)
	}
}
