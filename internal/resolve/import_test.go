package resolve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
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
		if fault, failed := downloadFailure(c.err, "Modrinth"); fault.why != c.why || failed != c.failed {
			t.Errorf("downloadFailure(%v) = %q, %v; want %q, %v", c.err, fault.why, failed, c.why, c.failed)
		}
	}
}

func TestAGroupCountsWhatItHolds(t *testing.T) {
	for _, c := range []struct {
		fetches []groupFetch
		want    string
	}{
		{[]groupFetch{{name: "a.jar", kind: manifest.TypeMod}, {name: "b.jar", kind: manifest.TypeMod}}, "Fetched 2 mods"},
		{[]groupFetch{{name: "a.zip", kind: manifest.TypeResourcePack}, {name: "b.zip", kind: manifest.TypeResourcePack}}, "Fetched 2 resource packs"},
		{[]groupFetch{{name: "a.jar", kind: manifest.TypeMod}, {name: "b.zip", kind: manifest.TypeShader}}, "Fetched 2 files"},
		{[]groupFetch{{name: "a.toml"}, {name: "b.toml"}}, "Fetched 2 files"},
	} {
		var stderr bytes.Buffer
		p := &out.Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
		r := &Resolver{Progress: p.Progress, Note: p.Note, Log: func(string, ...any) {}}
		end := r.startGroup(c.fetches)
		r.fetching("a", "1")
		r.fetching("b", "1")
		end(false)
		if got := stderr.String(); got != "  ✔ "+c.want+"\n" {
			t.Errorf("%v: stderr %q, want %q", c.fetches, got, c.want)
		}
	}
}

func TestAGroupHoldsTheStepsAndNotesItsLoopLogs(t *testing.T) {
	var stderr bytes.Buffer
	p := &out.Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	var steps []string
	r := &Resolver{Progress: p.Progress, Note: p.Note, Log: func(format string, args ...any) { steps = append(steps, fmt.Sprintf(format, args...)) }}
	end := r.startGroup([]groupFetch{{name: "sodium.jar", kind: manifest.TypeMod}, {name: "iris.jar", kind: manifest.TypeMod}})
	r.fetching("sodium", "0.9")
	r.log("switching iris from alpha to beta")
	r.noteKept("iris", "shaders", lock.Mod{VersionNumber: "1.8"})
	end(false)
	r.fetching("lithium", "0.15")
	if len(steps) != 1 || steps[0] != "fetching lithium 0.15" {
		t.Errorf("steps %q: only the fetch after the group is its own step", steps)
	}
	if stderr.String() != "  ✔ Fetched 1 mod\n  • iris 1.8 already in the pack (required by shaders)\n" {
		t.Errorf("stderr %q", stderr.String())
	}
}
