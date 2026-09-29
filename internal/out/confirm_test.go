package out

import (
	"strings"
	"testing"
)

func TestTheChosenAnswerHasABackground(t *testing.T) {
	h := confirmTheme(Theme{HasColor: true, GreyIndex: GreyDark})
	chosen, other := h.Focused.FocusedButton.Render("Yes"), h.Focused.BlurredButton.Render("No")
	if chosen != " \x1b[46m \x1b[m\x1b[30;46mYes\x1b[m\x1b[46m \x1b[m" {
		t.Fatalf("chosen %q isn't black on cyan", chosen)
	}
	if strings.Contains(other, "46") {
		t.Fatalf("other %q has a background", other)
	}
	if Width(chosen) != Width(other)+1 {
		t.Fatalf("chosen %q and other %q don't line up", chosen, other)
	}
}

func TestTheChosenAnswerIsBracketedWithoutColour(t *testing.T) {
	h := confirmTheme(Theme{GreyIndex: GreyDark})
	chosen, other := h.Focused.FocusedButton.Render("Yes"), h.Focused.BlurredButton.Render("No")
	if chosen != " [Yes]" || other != "  No " {
		t.Fatalf("chosen %q other %q", chosen, other)
	}
}
