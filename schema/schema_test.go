package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFixturesValidate(t *testing.T) {
	cases := map[string]Kind{
		"two-target.json": Manifest,
		"minimal.json":    Manifest,
		"lock.json":       Lock,
	}
	for name, kind := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(kind, fixture(t, name)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidRejected(t *testing.T) {
	cases := map[string]struct {
		kind Kind
		doc  string
	}{
		"manifest missing minecraft": {Manifest, `{"loader":{"type":"fabric"},"targets":{},"mods":{}}`},
		"lock pack with two pins":    {Lock, `{"lockVersion":1,"manifestSha256":"` + hex(64) + `","minecraft":"26.2","loader":{"type":"fabric","version":"0.17.0"},"java":{"major":21,"component":"java-runtime-delta"},"packs":{"../p":{"name":"p","commit":"` + hex(40) + `","dirSha256":"` + hex(64) + `"}},"mods":{},"players":[]}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(tc.kind, []byte(tc.doc)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func hex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
