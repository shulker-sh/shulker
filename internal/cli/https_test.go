package cli

import (
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
