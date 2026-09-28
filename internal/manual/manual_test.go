package manual

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func sums(data string) (sha1Sum, sha512Sum string) {
	a, b := sha1.Sum([]byte(data)), sha512.Sum512([]byte(data))
	return hex.EncodeToString(a[:]), hex.EncodeToString(b[:])
}

func TestOfReadsTheFilesAMissingFilesErrorCarries(t *testing.T) {
	files := []File{{Name: "a.jar", Page: "https://a", Sha1: "aa"}}
	e := out.Errorf("missing-files", "1 file needs a manual download")
	Attach(e, files)
	if got := Of(errors.Join(errors.New("other"), e)); len(got) != 1 || got[0] != files[0] {
		t.Fatalf("Of = %+v", got)
	}
	if got := Of(out.Errorf("missing-files", "no files")); got != nil {
		t.Fatalf("an error with no files attached has none: %+v", got)
	}
}

func TestCheckFindsAFileDroppedUnderAnyNameByEitherHash(t *testing.T) {
	downloads := t.TempDir()
	s1, _ := sums("one")
	_, s512 := sums("two")
	w := NewWait(downloads, []File{{Name: "one.jar", Sha1: s1}, {Name: "two.jar", Sha512: s512}})

	found, err := w.Check()
	if err != nil || found[0].Found || found[1].Found {
		t.Fatalf("nothing is there yet: %+v %v", found, err)
	}
	os.WriteFile(filepath.Join(downloads, "renamed.jar"), []byte("one"), 0o644)
	os.WriteFile(filepath.Join(downloads, "notes.txt"), []byte("two"), 0o644)
	found, err = w.Check()
	if err != nil || !found[0].Found || found[1].Found {
		t.Fatalf("a jar matches by sha1 under any name, and a .txt is never a candidate: %+v %v", found, err)
	}
	os.WriteFile(filepath.Join(downloads, "two.jar"), []byte("two"), 0o644)
	if found, err = w.Check(); err != nil || !found[1].Found {
		t.Fatalf("a jar matches by sha512: %+v %v", found, err)
	}
	if !w.Done() {
		t.Fatal("every file is found")
	}
}

func TestCheckWithoutADownloadsFolderFindsNothing(t *testing.T) {
	w := NewWait(filepath.Join(t.TempDir(), "downloads"), []File{{Name: "a.jar", Sha1: "aa"}})
	if found, err := w.Check(); err != nil || found[0].Found {
		t.Fatalf("%+v %v", found, err)
	}
}
