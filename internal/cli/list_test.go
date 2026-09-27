package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestListTagsOnlyAModThatSkipsADeclaredSide(t *testing.T) {
	both := []string{"client", "server"}
	for side, want := range map[string][]string{"": both, "both": both, "client": {"client"}} {
		if got := landsOn(side, both); !slices.Equal(got, want) {
			t.Errorf("landsOn(%q) = %v, want %v", side, got, want)
		}
	}
	if got := landsOn("server", []string{"client"}); got != nil {
		t.Errorf("a server mod in a client project lands on %v", got)
	}
}

func TestListWithNothingNamesTheCommandThatAdds(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	stdout := h.mustRun(t, "list")
	if !strings.Contains(stdout, "i No mods yet\n") || !strings.Contains(stdout, "Add a mod:") || !strings.Contains(stdout, "$ shulker add <mod>") {
		t.Errorf("empty list: %s", stdout)
	}
}
