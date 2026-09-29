package fetch

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteReadsAZipEntryByRanges(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "big.bin", Method: zip.Store})
	w.Write(bytes.Repeat([]byte("x"), 1<<20))
	w, _ = zw.Create("version.json")
	io.WriteString(w, `{"world_version":4903}`)
	zw.Close()
	var served int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &countingResponse{ResponseWriter: w, n: &served}
		http.ServeContent(cw, r, "server.jar", time.Time{}, bytes.NewReader(buf.Bytes()))
	}))
	defer srv.Close()

	f, err := trusting(srv).Remote(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(f, f.Size())
	if err != nil {
		t.Fatal(err)
	}
	rc, err := zr.Open("version.json")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	if string(got) != `{"world_version":4903}` {
		t.Fatalf("read %q", got)
	}
	if served > int64(buf.Len())/10 {
		t.Fatalf("served %d of %d bytes", served, buf.Len())
	}
}

func TestRemoteRefusesAServerThatIgnoresRanges(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		if r.Method == http.MethodGet {
			io.WriteString(w, "0123456789")
		}
	}))
	defer srv.Close()
	f, err := trusting(srv).Remote(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadAt(make([]byte, 4), 2); err == nil || !strings.Contains(err.Error(), "range") {
		t.Fatalf("a 200 answer to a range request: %v", err)
	}
}

type countingResponse struct {
	http.ResponseWriter
	n *int64
}

func (c *countingResponse) Write(p []byte) (int, error) {
	*c.n += int64(len(p))
	return c.ResponseWriter.Write(p)
}

func TestRemoteReadsRangesFromWhereItsURLRedirects(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 1<<14)
	mux := http.NewServeMux()
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/cdn/file", http.StatusFound)
	})
	mux.HandleFunc("/cdn/file", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "file", time.Time{}, bytes.NewReader(data))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	f, err := trusting(srv).Remote(context.Background(), srv.URL+"/file")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 8)
	if _, err := f.ReadAt(got, 8); err != nil || string(got) != "abcdefgh" {
		t.Fatalf("read %q, %v", got, err)
	}
}
