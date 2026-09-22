package zipfile

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture() map[string][]byte {
	return map[string][]byte{
		"pack.mcmeta":                      []byte(`{"pack":{"pack_format":64,"description":"golden"}}` + "\n"),
		"assets/minecraft/lang/en_us.json": []byte(strings.Repeat(`"menu.singleplayer": "Singleplayer",`+"\n", 200)),
		"assets/minecraft/texts/end.txt":   []byte("the end\n"),
		"License.txt":                      []byte(strings.Repeat("All rights reserved. ", 50)),
		"empty.txt":                        {},
	}
}

func TestBuildGolden(t *testing.T) {
	data, err := Build(fixture(), "")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(data)
	const want = "fd066fde47477fdffa135d4e2405e652530a52ed24ad53a45063abc07742bedd73ae7d68d83930de400394089381fe69414191a137e8c0274546d3d78008406c"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("sha512 changed, so a module or toolchain bump moved the deflate output:\ngot  %s\nwant %s", got, want)
	}
}

func TestBuildOrderAndHeaders(t *testing.T) {
	entries := fixture()
	data, err := Build(entries, "pack.mcmeta")
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
		if f.Method != zip.Deflate {
			t.Errorf("%s: method %d, want deflate", f.Name, f.Method)
		}
		if !f.Modified.Equal(Modified) {
			t.Errorf("%s: modified %s", f.Name, f.Modified)
		}
		if f.ExternalAttrs != 0 {
			t.Errorf("%s: external attrs %#x", f.Name, f.ExternalAttrs)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, entries[f.Name]) {
			t.Errorf("%s: contents differ", f.Name)
		}
	}
	want := "pack.mcmeta,License.txt,assets/minecraft/lang/en_us.json,assets/minecraft/texts/end.txt,empty.txt"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("order:\ngot  %s\nwant %s", got, want)
	}
}

func TestBuildIsRepeatable(t *testing.T) {
	a, err := Build(fixture(), "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(fixture(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of the same entries differ")
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFolderZipsEveryFileBySlashPath(t *testing.T) {
	root := writeTree(t, map[string]string{"pack.mcmeta": "{}", "assets/a/b.json": "b"})
	data, err := Folder(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Build(map[string][]byte{"pack.mcmeta": []byte("{}"), "assets/a/b.json": []byte("b")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("a folder's zip differs from Build over its files")
	}
}

func TestFolderLeavesOutJunk(t *testing.T) {
	files := map[string]string{"pack.mcmeta": "{}", "assets/a/b.json": "b"}
	clean, err := Folder(writeTree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	for _, junk := range []string{".DS_Store", "assets/.DS_Store", ".git/HEAD", "assets/.idea/x.xml", ".gitignore", "Thumbs.db", "assets/desktop.ini", "pack.mcmeta~", "assets/a/.b.json.swp", "assets/a/b.json.swp"} {
		files[junk] = "junk"
	}
	dirty, err := Folder(writeTree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(clean, dirty) {
		t.Fatal("excluded files changed the zip")
	}
}

func TestFolderFollowsASymlinkedRoot(t *testing.T) {
	root := writeTree(t, map[string]string{"pack.mcmeta": "{}"})
	link := filepath.Join(t.TempDir(), "pack")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	direct, err := Folder(root)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := Folder(link)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct, linked) {
		t.Fatal("a symlinked folder zips differently")
	}
}

func TestFolderFollowsSymlinksInside(t *testing.T) {
	root := writeTree(t, map[string]string{"pack.mcmeta": "{}"})
	shared := writeTree(t, map[string]string{"logo.png": "logo", "textures/a.png": "a"})
	for link, target := range map[string]string{"assets/logo.png": "logo.png", "assets/tex": "textures"} {
		if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(shared, target), filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	data, err := Folder(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Build(map[string][]byte{"pack.mcmeta": []byte("{}"), "assets/logo.png": []byte("logo"), "assets/tex/a.png": []byte("a")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("a symlink inside the folder zips as what it points at")
	}
	files, err := FolderFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	links := map[string]string{}
	for _, f := range files {
		links[f.Name] = f.Link
	}
	if links["pack.mcmeta"] != "" || links["assets/logo.png"] != "assets/logo.png" || links["assets/tex/a.png"] != "assets/tex" {
		t.Fatalf("each file names the link it was reached through: %v", links)
	}
}

func TestFolderRefusesALoopOrADanglingLink(t *testing.T) {
	loop := writeTree(t, map[string]string{"pack.mcmeta": "{}", "assets/a.png": "a"})
	if err := os.Symlink(loop, filepath.Join(loop, "assets", "up")); err != nil {
		t.Fatal(err)
	}
	dangling := writeTree(t, map[string]string{"pack.mcmeta": "{}"})
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.png"), filepath.Join(dangling, "gone.png")); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{loop, dangling} {
		if _, err := Folder(root); err == nil {
			t.Errorf("%s zipped", root)
		}
	}
}
