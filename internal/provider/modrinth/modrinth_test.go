package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"shulker.sh/shulker/internal/fetch"
)

func TestVersionsAsksForEveryLoader(t *testing.T) {
	var loaders string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loaders = r.URL.Query().Get("loaders")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	m := New(fetch.New("test"))
	m.BaseURL = srv.URL
	if _, err := m.Versions(context.Background(), "AANobbMI", "26.2", []string{"quilt", "fabric"}); err != nil {
		t.Fatal(err)
	}
	if loaders != `["quilt","fabric"]` {
		t.Errorf("loaders = %s", loaders)
	}
}
