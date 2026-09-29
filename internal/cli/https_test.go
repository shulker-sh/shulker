package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestInstallRefusesAPlainHTTPLockURL(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	var plain string
	h.editLock(t, func(l *lock.Lock) {
		m := l.Mods["sodium"]
		plain = "http://" + strings.TrimPrefix(*m.URL, "https://")
		m.URL = &plain
		l.Mods["sodium"] = m
	})
	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "install", "--json")
	if code == 0 || failureCode(t, stdout).Code != "url-insecure" || !strings.Contains(stdout, plain) {
		t.Fatalf("code=%d %s", code, stdout)
	}
}

func TestAPlainHTTPGitSourceIsRefused(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	for _, source := range []string{"http://127.0.0.1:1/pack.git", "git://127.0.0.1:1/pack.git"} {
		code, stdout, _ := h.run(t, "modpack", "add", source, "--json")
		if e := failureCode(t, stdout); code == 0 || e.Code != "url-insecure" || e.Protection != "https" || !strings.Contains(stdout, source) {
			t.Fatalf("%s: code=%d %s", source, code, stdout)
		}
	}
	_, _, stderr := h.run(t, "modpack", "add", "http://127.0.0.1:1/pack.git")
	if !strings.Contains(stderr, "Read what shulker checks and why:\n    $ shulker security\n") {
		t.Fatalf("a refusal points at shulker security: %q", stderr)
	}
}

func TestAGitSourceThatRedirectsToPlainHTTPIsRefused(t *testing.T) {
	h := newInPlace(t)
	g := serveGitPack(t, "alpha", `"sodium": {}`)
	plain := httptest.NewServer(g.srv.Config.Handler)
	t.Cleanup(plain.Close)
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(redirect.Close)

	code, stdout, _ := h.run(t, "modpack", "add", redirect.URL+"/alpha.git", "--json")
	if code == 0 {
		t.Fatalf("a redirect to plain http must fail the clone: %s", stdout)
	}
	if h.readManifest(t).Requires["alpha"].Source != "" {
		t.Fatal("a refused source stays out of the manifest")
	}
}
