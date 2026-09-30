package out

import (
	"errors"
	"io"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Confirm puts a yes-or-no question on one line, with Yes preselected when yes is set and No
// otherwise, so a stray enter takes the safe answer. Esc is No; only ctrl-c leaves it unanswered.
func (p *Printer) Confirm(question string, yes bool, in io.Reader) (bool, error) {
	t := p.ErrTheme
	keys := huh.NewDefaultKeyMap()
	keys.Confirm.Toggle = key.NewBinding(key.WithKeys("left", "right", "h", "l", "tab", "space"))
	keys.Confirm.Reject = key.NewBinding(key.WithKeys("n", "N", "esc"))
	field := huh.NewConfirm().Title(gutter + question).Affirmative("Yes").Negative("No").Inline(true).WithButtonAlignment(lipgloss.Left).Value(&yes)
	p.openPrompt()
	form := huh.NewForm(huh.NewGroup(field)).WithTheme(formTheme(confirmTheme(t))).WithProgramOptions(p.drawOptions()...).WithOutput(p.Stderr).WithInput(in).
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
func confirmTheme(t Theme) *huh.Styles {
	h := pickTheme(t)
	chosen := t.Style().MarginLeft(1).Transform(func(s string) string { return "[" + s + "]" })
	other := t.Style().MarginLeft(1).Transform(func(s string) string { return " " + s + " " })
	if t.HasColor {
		chosen = t.Style().MarginLeft(1).Padding(0, 1).Foreground(lipgloss.Black).Background(lipgloss.Cyan)
		other = t.StyleGrey().MarginLeft(1).Padding(0, 1)
	}
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.FocusedButton, f.BlurredButton = chosen, other
	}
	return h
}
