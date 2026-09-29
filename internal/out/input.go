package out

import (
	"errors"
	"io"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Ask reads one line of free text, for a value no list can offer. It draws on stderr beside the
// pickers, with description under the question, and returns the answer trimmed. An empty answer
// is placeholder, which shows greyed in the empty line.
func (p *Printer) Ask(title, description, placeholder string, in io.Reader) (string, error) {
	t := p.ErrTheme
	var answer string
	field := huh.NewInput().Title(gutter + title).Value(&answer).Prompt(gutter + "> ").Placeholder(placeholder)
	// The question, the typed line, and the blank row above the key help.
	height := 3
	if description != "" {
		field, height = field.Description(gutter+description), height+1
	}
	// A form left to size itself gives the group no room for the field, which draws the question
	// as blank lines with only the key help under them.
	p.openPrompt()
	form := huh.NewForm(huh.NewGroup(field)).WithTheme(formTheme(askTheme(t))).WithProgramOptions(p.drawOptions()...).WithOutput(p.Stderr).WithInput(in).WithWidth(p.width()).WithHeight(height)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrPickCancelled
		}
		return "", err
	}
	if answer = strings.TrimSpace(answer); answer == "" {
		return placeholder, nil
	}
	return answer, nil
}

// askTheme is the picker's theme with the typed line added: the prompt in cyan, like the pick
// arrow it stands in for, and the text itself in the terminal's own colour.
func askTheme(t Theme) *huh.Styles {
	h := pickTheme(t)
	plain := lipgloss.NewStyle()
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.TextInput.Prompt, f.TextInput.Text = plain, plain
		if t.HasColor {
			f.TextInput.Prompt = plain.Foreground(lipgloss.Cyan)
		}
	}
	return h
}
