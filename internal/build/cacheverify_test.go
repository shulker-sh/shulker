package build

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/provider"
)

func TestVerifyNamesEveryRootThatLocksAGoneFile(t *testing.T) {
	e := envtest.New(t)
	gone := e.Modrinth.Publish(provider.Project{ID: "gone-id", Slug: "gone"}, provider.Version{File: provider.File{Filename: "gone.jar"}}, []byte("gone"))
	kept := e.Modrinth.Publish(provider.Project{ID: "kept-id", Slug: "kept"}, provider.Version{File: provider.File{Filename: "kept.jar"}}, []byte("kept"))
	e.Modrinth.Files = slices.DeleteFunc(e.Modrinth.Files, func(v provider.Version) bool { return v.ID == gone.ID })
	locked := func(vs ...provider.Version) *lock.Lock {
		l := lock.New()
		for _, v := range vs {
			l.Mods[v.ProjectID] = lock.Mod{Provider: "modrinth", Project: v.ProjectID, Version: v.ID, Filename: v.File.Filename, Sha512: v.File.Sha512}
		}
		return l
	}
	roots := Roots{Locks: []cache.Root{
		{Lock: locked(kept), Name: "Friends"},
		{Lock: locked(kept, gone), Name: "Friends (history 2)"},
		{Lock: locked(gone), Name: "/projects/pack"},
	}}

	v, err := roots.Verify(context.Background(), e.Cache, e.Providers, false)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Fails() || len(v.Takedowns) != 1 || !slices.Equal(v.Takedowns[0].Roots, []string{"Friends (history 2)", "/projects/pack"}) || !slices.Equal(v.Takedowns[0].Keys, []string{"gone-id"}) {
		t.Fatalf("takedowns: %+v", v.Takedowns)
	}
	if e.Modrinth.Requests["Filed"] != 1 {
		t.Fatalf("one request for every root: %v", e.Modrinth.Requests)
	}
}

func TestVerifyDropsChangedObjectsOnlyWhenAsked(t *testing.T) {
	e := envtest.New(t)
	sha, err := e.Cache.Put(strings.NewReader("jar"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.Cache.Object(sha), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := Roots{}.Verify(context.Background(), e.Cache, e.Providers, false)
	if err != nil || !v.Fails() || len(v.Changed) != 1 || v.Dropped || !e.Cache.Has(sha) || len(v.Unused) != 1 {
		t.Fatalf("a changed object fails the check and stays: %+v %v", v, err)
	}
	v, err = Roots{}.Verify(context.Background(), e.Cache, e.Providers, true)
	if err != nil || v.Fails() || !v.Dropped || e.Cache.Has(sha) || len(v.Unused) != 0 {
		t.Fatalf("a dropped changed object no longer fails it: %+v %v", v, err)
	}
}
