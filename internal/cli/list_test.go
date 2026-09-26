package cli

import (
	"slices"
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
