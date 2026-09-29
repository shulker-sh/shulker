package out

import (
	"errors"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Confirm puts a yes-or-no question on one line, No preselected so a stray enter declines. Esc is
// No as well; only ctrl-c leaves it unanswered.
func (p *Printer) Confirm(question string, in io.Reader) (bool, error) {
	t := p.ErrTheme
	if !t.HasColor {
		// lipgloss reads the terminal itself, so --no-color has to reach it separately.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	var yes bool
	keys := huh.NewDefaultKeyMap()
	keys.Confirm.Toggle = key.NewBinding(key.WithKeys("left", "right", "h", "l", "tab"))
	keys.Confirm.Reject = key.NewBinding(key.WithKeys("n", "N", "esc"))
	field := huh.NewConfirm().Title(gutter + question).Affirmative("Yes").Negative("No").Inline(true).WithButtonAlignment(lipgloss.Left).Value(&yes)
	p.openPrompt()
	form := huh.NewForm(huh.NewGroup(field)).WithTheme(confirmTheme(t)).WithOutput(p.Stderr).WithInput(in).
		WithKeyMap(keys).WithShowHelp(false).WithWidth(p.width()).WithHeight(1)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, ErrPickCancelled
		}
		return false, err
	}
	return yes, nil
}

// confirmTheme is the picker's theme with the two answers after the question: the chosen one on a
// cyan background, like the pick arrow's colour, the other grey. Without colour the chosen one is
// bracketed.
func confirmTheme(t Theme) *huh.Theme {
	h := pickTheme(t)
	chosen := t.Style().MarginLeft(1).Transform(func(s string) string { return "[" + s + "]" })
	other := t.Style().MarginLeft(1).Transform(func(s string) string { return " " + s + " " })
	if t.HasColor {
		chosen = t.Style().MarginLeft(1).Padding(0, 1).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
		other = t.Style().MarginLeft(1).Padding(0, 1).Foreground(t.lipglossGrey())
	}
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.FocusedButton, f.BlurredButton = chosen, other
	}
	return h
}
