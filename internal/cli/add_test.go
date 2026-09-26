package cli

import (
	"strings"
	"testing"
)

func TestAddDoesNotNudgeAnInstallOfWhatItFetched(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	if stdout := h.mustRun(t, "add", "sodium"); !strings.Contains(stdout, "+ sodium") || strings.Contains(stdout, "shulker install") {
		t.Fatalf("add: %s", stdout)
	}
}
