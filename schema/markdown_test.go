package schema

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the generated pages in site/docs")

func TestDocsPagesMatchSchemas(t *testing.T) {
	for _, page := range docsPages {
		got, err := renderDocsPage(page)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("..", "site", "docs", page.name)
		if *update {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("site/docs/%s is out of date with schema/%s; run: go test ./schema -run TestDocs -update", page.name, page.kind)
		}
	}
}
