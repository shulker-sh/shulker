package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	url := srv.URL + "/x"
	c := New("test")
	if err := c.GetJSON(context.Background(), url, &struct{}{}); err == nil || IsNetwork(err) {
		t.Fatalf("an HTTP error is not a network error: %v", err)
	}
	srv.Close()
	if err := c.GetJSON(context.Background(), url, &struct{}{}); !IsNetwork(err) {
		t.Fatalf("a refused connection is a network error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.GetJSON(ctx, url, &struct{}{}); err == nil || IsNetwork(err) {
		t.Fatalf("a cancelled request is not a network error: %v", err)
	}
	c.Offline = true
	if err := c.GetJSON(context.Background(), "http://example.invalid/", &struct{}{}); !errors.Is(err, ErrOffline) || !IsNetwork(err) {
		t.Fatalf("offline mode: %v", err)
	}
	if !IsNetwork(fmt.Errorf("wrapped: %w", Unreachable(errors.New("could not resolve host")))) {
		t.Fatal("Unreachable marks an error as a network error")
	}
}
