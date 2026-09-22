package resolve

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

func TestDownloadFailure(t *testing.T) {
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(9189206))
		w.Write(make([]byte, 344<<10))
	}))
	defer cdn.Close()
	_, truncated := fetch.New("test").Download(context.Background(), cdn.URL+"/x.jar", io.Discard)
	for _, c := range []struct {
		err    error
		why    string
		failed bool
	}{
		{truncated, "the file was cut short", true},
		{&fetch.StatusError{URL: "https://cdn/x.jar", Status: 502}, "HTTP 502", true},
		{out.Errorf("checksum-mismatch", "the download doesn't match"), "the file doesn't match its hash", true},
		{out.Errorf("manual-download", "x is not distributed"), "it refused the download", true},
		{fmt.Errorf("https://cdn/x.jar: %w", fetch.ErrOffline), "", false},
		{&fs.PathError{Op: "write", Path: "/cache/obj", Err: fmt.Errorf("no space left on device")}, "", false},
		{out.Errorf("requires-taken", "requires already has x"), "", false},
		{nil, "", false},
	} {
		if why, failed := downloadFailure(c.err); why != c.why || failed != c.failed {
			t.Errorf("downloadFailure(%v) = %q, %v; want %q, %v", c.err, why, failed, c.why, c.failed)
		}
	}
}
