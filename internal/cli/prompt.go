package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"shulker.sh/shulker/internal/out"
)

// ask puts one question with a list of answers. Every prompt goes through here and through
// canPick before it: a step under way is settled first, because a spinner and a form would
// otherwise draw over each other on stderr.
func (a *app) ask(title string, choices []out.Choice) (string, error) {
	a.printer.Settle()
	answer, err := a.questions().Pick(title, choices, a.stdin)
	return answer, escaped(err)
}

// askText puts a question no list can answer, like a path or a URL. An empty answer is
// placeholder.
func (a *app) askText(title, description, placeholder string) (string, error) {
	a.printer.Settle()
	answer, err := a.questions().Ask(title, description, placeholder, a.stdin)
	return answer, escaped(err)
}

// confirm asks before something that can't be undone. Off a terminal there is nobody to ask, so
// --yes is required there instead: doing nothing is the safe default, and a script that means it
// says so.
func (a *app) confirm(question string) (bool, error) {
	if !a.yes && !a.canPick() {
		e := out.Errorf("usage", "shulker asks before this, and can't ask here")
		e.Help = "pass --yes to answer it"
		return false, e
	}
	return a.askYes(question)
}

// askYes puts a yes-or-no question with No preselected, so a stray enter declines. --yes answers
// it without asking.
func (a *app) askYes(question string) (bool, error) {
	if a.yes {
		return true, nil
	}
	a.printer.Settle()
	yes, err := a.questions().Confirm(question, false, a.stdin)
	return yes, escaped(err)
}

// askYesFirst is askYes with Yes preselected, for a question most answer yes to.
func (a *app) askYesFirst(question string) (bool, error) {
	if a.yes {
		return true, nil
	}
	a.printer.Settle()
	yes, err := a.questions().Confirm(question, true, a.stdin)
	return yes, escaped(err)
}

// yesFlag gives cmd the --yes that answers each confirm it puts; usage says what Yes does there.
func (a *app) yesFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().BoolVarP(&a.yes, "yes", "y", false, usage)
}

// asksYes reports whether a question can be answered, by a person or by --yes.
func (a *app) asksYes() bool { return a.yes || a.canPick() }

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
