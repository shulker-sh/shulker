package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/out"
)

func TestIsNetwork(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	url := srv.URL + "/x"
	c := trusting(srv)
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

func TestRateLimitedSaysWhenToRetry(t *testing.T) {
	for header, want := range map[string]time.Duration{"X-Ratelimit-Reset": 7 * time.Second, "Retry-After": 3 * time.Second} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(header, strconv.Itoa(int(want/time.Second)))
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		err := trusting(srv).GetJSON(context.Background(), srv.URL, &struct{}{})
		srv.Close()
		var se *StatusError
		if !errors.Is(err, ErrRateLimited) || !errors.As(err, &se) || se.RetryAfter != want {
			t.Errorf("%s: %v", header, err)
		}
	}
}

func noRetryWaits(t *testing.T) {
	saved := retryWaits
	retryWaits = []time.Duration{0, 0}
	t.Cleanup(func() { retryWaits = saved })
}

// dropping answers after closing the connection unanswered on the first drops requests, as a
// server closing a kept-alive connection does.
func dropping(t *testing.T, drops int) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= drops {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
			return
		}
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, `{"method": %q, "body": %q}`, r.Method, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestADroppedConnectionIsRetried(t *testing.T) {
	noRetryWaits(t)
	srv, calls := dropping(t, 2)
	c := trusting(srv)
	var got struct{ Method, Body string }
	if err := c.PostJSON(context.Background(), srv.URL, map[string]int{"n": 1}, &got); err != nil {
		t.Fatal(err)
	}
	if *calls != 3 || got.Method != "POST" || got.Body != `{"n":1}` {
		t.Fatalf("calls %d, got %+v", *calls, got)
	}
}

func TestADroppedConnectionFailsOnceTheRetriesRunOut(t *testing.T) {
	noRetryWaits(t)
	srv, calls := dropping(t, 3)
	c := trusting(srv)
	var buf bytes.Buffer
	_, err := c.Download(context.Background(), srv.URL+"/a.jar", &buf)
	if err == nil || !IsNetwork(err) || *calls != 3 {
		t.Fatalf("calls %d, err %v", *calls, err)
	}
}

func TestAServerErrorIsNotRetried(t *testing.T) {
	noRetryWaits(t)
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := trusting(srv).GetJSON(context.Background(), srv.URL, &struct{}{}); err == nil || calls != 1 {
		t.Fatalf("calls %d, err %v", calls, err)
	}
}

// trusting is a client for srv, a TLS test server.
func trusting(srv *httptest.Server) *Client {
	c := New("test")
	c.HTTP = srv.Client()
	return c
}

func TestPlainHTTPIsRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jar"))
	}))
	defer plain.Close()
	srv := httptest.NewTLSServer(http.RedirectHandler(plain.URL+"/a.jar", http.StatusFound))
	defer srv.Close()
	c := trusting(srv)
	for _, url := range []string{plain.URL + "/a.jar", srv.URL + "/a.jar"} {
		_, err := c.Download(context.Background(), url, io.Discard)
		if out.CodeOf(err) != "url-insecure" || !strings.Contains(err.Error(), plain.URL+"/a.jar") {
			t.Fatalf("%s: want url-insecure naming the plain URL, got %v", url, err)
		}
	}
	if _, err := c.Remote(context.Background(), plain.URL+"/a.jar"); out.CodeOf(err) != "url-insecure" {
		t.Fatalf("remote: %v", err)
	}
}
