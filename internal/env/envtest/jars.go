package envtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"testing"
)

// ZipFiles is a zip holding the given files.
func ZipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ModJar is a fabric mod jar whose fabric.mod.json names id and version and runs on env: client,
// server or *.
func ModJar(t *testing.T, id, version, env string) []byte {
	t.Helper()
	return ZipFiles(t, map[string]string{"fabric.mod.json": fmt.Sprintf(`{"id":%q,"version":%q,"environment":%q,"depends":{"fabricloader":">=0.17"}}`, id, version, env)})
}
