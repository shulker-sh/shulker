package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

func TestFetchFailureKeepsNetwork(t *testing.T) {
	c := fetch.New("test")
	c.Offline = true
	_, err := NewFabric(c).LoaderVersions(context.Background(), "1.21.1")
	if out.CodeOf(err) != "meta-fetch" || !fetch.IsNetwork(err) {
		t.Fatalf("offline: code %q, network %v", out.CodeOf(err), fetch.IsNetwork(err))
	}
	if rows := out.AsError(err).Rows; len(rows) != 1 || rows[0].Label != "fabric" {
		t.Errorf("rows %+v", rows)
	}
}

func TestServerErrorIsNotNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := NewPiston(fetch.New("test"))
	p.ManifestURL = srv.URL
	_, err := p.ServerDownload(context.Background(), "1.21.1")
	if out.CodeOf(err) != "meta-fetch" || fetch.IsNetwork(err) {
		t.Fatalf("500: code %q, network %v", out.CodeOf(err), fetch.IsNetwork(err))
	}
}

func TestUnlistedGameIsInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"versions":[]}`))
	}))
	defer srv.Close()
	p := NewPiston(fetch.New("test"))
	p.ManifestURL = srv.URL
	if _, err := p.ServerDownload(context.Background(), "1.21.1"); out.CodeOf(err) != "meta-invalid" {
		t.Fatalf("code %q", out.CodeOf(err))
	}
}
