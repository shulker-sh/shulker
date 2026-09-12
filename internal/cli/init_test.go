package cli

import (
	"os"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestInitChecksTheTarget(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "init", "--yes", "--target", "weird", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || len(e.Candidates) != 2 {
		t.Fatalf("unknown target: code=%d %s", code, stdout)
	}
}

func TestFailedInitLeavesNothingBehind(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run(t, "init", "--yes", "--name", "..."); code == 0 {
		t.Fatal("an invalid name must fail")
	}
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("a failed init left %s behind", e.Name())
	}
}
