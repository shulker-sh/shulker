package manual

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/cache"
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
	w := NewWait(downloads, nil, []File{{Name: "one.jar", Sha1: s1}, {Name: "two.jar", Sha512: s512}})

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
	if found, err = w.Check(); err != nil || !found[0].Found || !found[1].Found {
		t.Fatalf("a jar matches by sha512, and a file once found stays found: %+v %v", found, err)
	}
}

func TestCheckWithoutADownloadsFolderFindsNothing(t *testing.T) {
	w := NewWait(filepath.Join(t.TempDir(), "downloads"), []string{filepath.Join(t.TempDir(), "gone")}, []File{{Name: "a.jar", Sha1: "aa"}})
	if found, err := w.Check(); err != nil || found[0].Found {
		t.Fatalf("%+v %v", found, err)
	}
}

// watched is a project's downloads folder and a watched folder beside it, with the file the wait
// wants, a.jar, published with the bytes "right".
func watched(t *testing.T) (downloads, folder string, file File) {
	t.Helper()
	s1, _ := sums("right")
	return filepath.Join(t.TempDir(), "downloads"), t.TempDir(), File{Name: "a.jar", Page: "https://a", Sha1: s1}
}

func write(t *testing.T, path, data string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	os.Chtimes(path, at, at)
}

func landed(t *testing.T, downloads string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(downloads, "a.jar"))
	if err != nil {
		t.Fatalf("nothing landed in downloads/: %v", err)
	}
	return string(data)
}

func TestCheckCopiesANameMatchThatWasAlreadyInAWatchedFolder(t *testing.T) {
	downloads, folder, file := watched(t)
	write(t, filepath.Join(folder, "a.jar"), "right", time.Hour)
	w := NewWait(downloads, []string{folder}, []File{file})
	if found, err := w.Check(); err != nil || !found[0].Found {
		t.Fatalf("%+v %v", found, err)
	}
	if landed(t, downloads) != "right" {
		t.Fatal("the file lands in downloads/")
	}
	if _, err := os.Stat(filepath.Join(folder, "a.jar")); err != nil {
		t.Fatalf("a file that predates the wait is copied, not moved: %v", err)
	}
}

func TestCheckTakesTheNewestBrowserDuplicateThatMatches(t *testing.T) {
	downloads, folder, file := watched(t)
	write(t, filepath.Join(folder, "a.jar"), "wrong", 3*time.Hour)
	write(t, filepath.Join(folder, "a (1).jar"), "right", 2*time.Hour)
	write(t, filepath.Join(folder, "a (2).jar"), "also wrong", time.Hour)
	w := NewWait(downloads, []string{folder}, []File{file})
	if found, err := w.Check(); err != nil || !found[0].Found || found[0].Note != "" {
		t.Fatalf("%+v %v", found, err)
	}
	if landed(t, downloads) != "right" {
		t.Fatal("the duplicate that matches lands under the expected name")
	}
}

func TestCheckTakesAnyOtherFileOnlyOnceTheWaitHasStarted(t *testing.T) {
	downloads, folder, file := watched(t)
	write(t, filepath.Join(folder, "renamed.jar"), "right", time.Hour)
	w := NewWait(downloads, []string{folder}, []File{file})
	if found, _ := w.Check(); found[0].Found {
		t.Fatal("a file under another name that was there before the wait isn't a candidate")
	}
	write(t, filepath.Join(folder, "renamed-later.jar"), "right", 0)
	if found, err := w.Check(); err != nil || !found[0].Found {
		t.Fatalf("one that arrives during the wait is: %+v %v", found, err)
	}
	if landed(t, downloads) != "right" {
		t.Fatal("it lands under the expected name")
	}
	if _, err := os.Stat(filepath.Join(folder, "renamed-later.jar")); !os.IsNotExist(err) {
		t.Fatalf("a file that arrived during the wait is moved: %v", err)
	}
}

func TestCheckLeavesANameMatchWithOtherBytesAndSaysSo(t *testing.T) {
	downloads, folder, file := watched(t)
	write(t, filepath.Join(folder, "a.jar"), "wrong", time.Hour)
	w := NewWait(downloads, []string{folder}, []File{file})
	found, err := w.Check()
	if err != nil || found[0].Found || found[0].Note != "a.jar in "+folder+" isn't the expected file" {
		t.Fatalf("%+v %v", found, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "a.jar")); err != nil {
		t.Fatalf("the file stays where it is: %v", err)
	}
	if _, err := os.Stat(filepath.Join(downloads, "a.jar")); !os.IsNotExist(err) {
		t.Fatalf("nothing lands: %v", err)
	}
}

func TestCheckDropsTheNoteOnceTheWrongCopyIsGone(t *testing.T) {
	downloads, folder, file := watched(t)
	write(t, filepath.Join(folder, "a.jar"), "wrong", time.Hour)
	w := NewWait(downloads, []string{folder}, []File{file})
	if found, err := w.Check(); err != nil || found[0].Note == "" {
		t.Fatalf("%+v %v", found, err)
	}
	os.Remove(filepath.Join(folder, "a.jar"))
	if found, err := w.Check(); err != nil || found[0].Found || found[0].Note != "" {
		t.Fatalf("%+v %v", found, err)
	}
}

func TestCheckFindsAFileByItsOwnExtension(t *testing.T) {
	downloads := t.TempDir()
	_, s512 := sums("pack")
	w := NewWait(downloads, nil, []File{{Name: "pack-1.0.mrpack", Sha512: s512}})
	os.WriteFile(filepath.Join(downloads, "renamed.mrpack"), []byte("pack"), 0o644)
	if found, err := w.Check(); err != nil || !found[0].Found {
		t.Fatalf("a modpack's .mrpack is a candidate: %+v %v", found, err)
	}
}

func TestPastedPathsReadsWhatATerminalDropsIn(t *testing.T) {
	for text, want := range map[string][]string{
		"/tmp/a.jar\n":                        {"/tmp/a.jar"},
		`/tmp/My\ Mods/a\ (1).jar /tmp/b.jar`: {"/tmp/My Mods/a (1).jar", "/tmp/b.jar"},
		`'/tmp/My Mods/a.jar'`:                {"/tmp/My Mods/a.jar"},
		`"/tmp/My Mods/a.jar"`:                {"/tmp/My Mods/a.jar"},
		"file:///tmp/My%20Mods/a.jar":         {"/tmp/My Mods/a.jar"},
	} {
		if got := pastedPaths(text); !slices.Equal(got, want) {
			t.Errorf("pastedPaths(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestTakeHashesAPastedPathAgainstTheMissingFiles(t *testing.T) {
	downloads, folder, file := watched(t)
	right := filepath.Join(folder, "My Mods", "renamed.jar")
	os.MkdirAll(filepath.Dir(right), 0o755)
	write(t, right, "right", time.Hour)
	write(t, filepath.Join(folder, "other.jar"), "other", time.Hour)
	w := NewWait(downloads, nil, []File{file})

	found, note, err := w.Take(filepath.Join(folder, "other.jar"))
	if err != nil || found[0].Found || note != filepath.Join(folder, "other.jar")+" isn't one of the files" {
		t.Fatalf("a file that matches nothing is noted: %+v %q %v", found, note, err)
	}
	found, note, err = w.Take(strings.ReplaceAll(right, " ", `\ `))
	if err != nil || !found[0].Found || note != "" {
		t.Fatalf("%+v %q %v", found, note, err)
	}
	if landed(t, downloads) != "right" {
		t.Fatal("it lands under the expected name")
	}
	if _, err := os.Stat(right); err != nil {
		t.Fatalf("a file that predates the wait is copied: %v", err)
	}
	if _, note, _ := w.Take(filepath.Join(folder, "gone.jar")); note != filepath.Join(folder, "gone.jar")+" isn't a file" {
		t.Fatalf("a path to nothing is noted: %q", note)
	}
}

func TestAFoundFileSaysWhichFolderItWasFoundIn(t *testing.T) {
	downloads, folder, file := watched(t)
	s1, _ := sums("mine")
	other := File{Name: "b.jar", Sha1: s1}
	s1, _ = sums("dropped")
	pasted := File{Name: "c.jar", Sha1: s1}
	w := NewWait(downloads, []string{folder}, []File{file, other, pasted})
	os.MkdirAll(downloads, 0o755)
	write(t, filepath.Join(folder, "a.jar"), "right", time.Hour)
	write(t, filepath.Join(downloads, "b.jar"), "mine", time.Hour)
	drop := filepath.Join(t.TempDir(), "c.jar")
	write(t, drop, "dropped", time.Hour)
	if _, _, err := w.Take(drop); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		found, err := w.Check()
		if err != nil || found[0].From != folder || found[1].From != downloads || !found[2].Found || found[2].From != "" {
			t.Fatalf("a find keeps the folder it was first found in, and a dropped file has none: %+v %v", found, err)
		}
	}
}

func TestCheckFindsAFileTheCacheHolds(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	s1, _ := sums("by sha1")
	_, s512 := sums("by sha512")
	w := NewWait(t.TempDir(), nil, []File{{Name: "a.jar", Sha1: s1}, {Name: "b.mrpack", Sha512: s512}})
	w.Cache = c
	if found, err := w.Check(); err != nil || found[0].Found || found[1].Found {
		t.Fatalf("the cache holds neither yet: %+v %v", found, err)
	}
	c.PutManual(strings.NewReader("by sha1"))
	c.Put(strings.NewReader("by sha512"))
	found, err := w.Check()
	if err != nil || !found[0].Found || !found[0].Cached || !found[1].Found || !found[1].Cached {
		t.Fatalf("a file that lands in the cache is found there: %+v %v", found, err)
	}
}
