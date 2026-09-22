package zipfile

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"io"
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
