package cli

import (
	"errors"

	"shulker.sh/shulker/internal/out"
)

// ask puts one question with a list of answers. Every prompt goes through here and through
// canPick before it: a step under way is settled first, because a spinner and a form would
// otherwise draw over each other on stderr.
func (a *app) ask(title string, choices []out.Choice) (string, error) {
	a.printer.Settle()
	answer, err := a.printer.Pick(title, choices, a.stdin)
	return answer, escaped(err)
}

// askText puts a question no list can answer, like a path or a URL.
func (a *app) askText(title, description string) (string, error) {
	a.printer.Settle()
	answer, err := a.printer.Ask(title, description, a.stdin)
	return answer, escaped(err)
}

// confirm asks before something that can't be undone. Off a terminal there is nobody to ask, so
// the flag that answers the question is required there instead: doing nothing is the safe default,
// and a script that means it says so.
func (a *app) confirm(question, flag string) (bool, error) {
	if !a.canPick() {
		return false, out.Errorf("usage", "shulker asks before this, and can't ask here; pass %s to answer it", flag)
	}
	answer, err := a.ask(question, []out.Choice{{Label: "no", Value: "no"}, {Label: "yes", Value: "yes"}})
	return answer == "yes", err
}

// escaped is what leaving a wizard half-answered means: the run ends where ctrl-c would leave
// it, with nothing created and nothing to read.
func escaped(err error) error {
	if errors.Is(err, out.ErrPickCancelled) {
		return &out.Error{Code: "interrupted", Message: "interrupted", Exit: out.ExitInterrupted}
	}
	return err
}

// latestChoice is the row every version question opens with: the range that always resolves to
// the newest, and what that is today.
func latestChoice(t out.Theme, current string) out.Choice {
	return out.Choice{Label: "latest" + t.Aside("currently "+current), Value: "*"}
}
