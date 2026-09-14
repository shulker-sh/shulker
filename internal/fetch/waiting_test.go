package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWaiting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	var started []string
	open := 0
	c := New("test")
	c.Waiting = func(host string) func() {
		started = append(started, host)
		open++
		return func() { open-- }
	}
	var v struct{ OK bool }
	if err := c.GetJSON(context.Background(), srv.URL+"/", &v); err != nil || !v.OK {
		t.Fatalf("get: %v %v", v, err)
	}
	if err := c.GetJSON(context.Background(), srv.URL+"/missing", &v); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if len(started) != 2 || started[0] != "127.0.0.1" || open != 0 {
		t.Fatalf("started %v, still open %d", started, open)
	}

	c.HTTP = &http.Client{Transport: failingTransport{}}
	if err := c.GetJSON(context.Background(), srv.URL+"/", &v); err == nil || open != 0 {
		t.Fatalf("a failed request should end its wait: %v, open %d", err, open)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("no route")
}
