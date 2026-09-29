package cache

import (
	"os"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestRehashFindsAnObjectWhoseBytesChanged(t *testing.T) {
	c := newCache(t)
	kept := object(t, c, "kept")
	tampered := object(t, c, "tampered")
	f, err := os.OpenFile(c.Object(tampered), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(" and more")
	f.Close()

	changed, hashed, err := c.Rehash()
	if err != nil {
		t.Fatal(err)
	}
	if hashed != 2 || len(changed) != 1 || changed[0] != (Object{Sha512: tampered, Size: int64(len("tampered and more"))}) {
		t.Fatalf("changed %+v of %d", changed, hashed)
	}

	if err := c.Drop(tampered); err != nil || c.Has(tampered) || !c.Has(kept) {
		t.Fatalf("drop removes only the changed object: %v", err)
	}
}

func TestUnusedListsObjectsNoRootReferencesButManualDownloads(t *testing.T) {
	c := newCache(t)
	used := object(t, c, "used")
	unused := object(t, c, "unused")
	manual := object(t, c, "manual")
	if err := c.MarkManual(manual); err != nil {
		t.Fatal(err)
	}
	l := lock.New()
	l.Mods["used"] = lock.Mod{Sha512: used}

	objects, err := c.Unused([]Root{{Lock: l}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(objects, []Object{{Sha512: unused, Size: int64(len("unused"))}}) {
		t.Fatalf("unused: %+v", objects)
	}
}
