package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffMatchesGit(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff binary")
	}
	cases := []struct{ a, b string }{
		{"a\nb\nc\n", "a\nB\nc\n"},
		{"", "one\ntwo\n"},
		{"one\ntwo\n", ""},
		{"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n", "1\n2\nX\n4\n5\n6\n7\n8\n9\n10\nY\n12\n"},
		{"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n", "1\n2\nX\n4\n5\n6\n7\n8\n9\n10\n11\n12\nZ\n"},
		{"a\nb", "a\nb\nc\n"},
		{"x\ny\nz\n", "z\ny\nx\n"},
	}
	dir := t.TempDir()
	for i, c := range cases {
		pa, pb := filepath.Join(dir, "a"), filepath.Join(dir, "b")
		os.WriteFile(pa, []byte(c.a), 0o644)
		os.WriteFile(pb, []byte(c.b), 0o644)
		raw, _ := exec.Command("diff", "-u", "--label", "a/f", "--label", "b/f", pa, pb).Output()
		want := string(raw)
		got := unifiedDiff("f", []byte(c.a), []byte(c.b))
		if strings.TrimSpace(got) != strings.TrimSpace(want) {
			t.Errorf("case %d\nwant:\n%s\ngot:\n%s", i, want, got)
		}
	}
	if unifiedDiff("f", []byte("same\n"), []byte("same\n")) != "" {
		t.Error("identical input should produce no diff")
	}
	if !strings.Contains(unifiedDiff("f", []byte("a\x00b"), []byte("c")), "Binary") {
		t.Error("binary input should be reported as binary")
	}
}
