package out

import (
	"errors"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Ask reads one line of free text, for a value no list can offer. It draws on stderr beside the
// pickers, with description under the question, and returns the answer trimmed.
func (p *Printer) Ask(title, description string, in io.Reader) (string, error) {
	t := p.ErrTheme
	if !t.Color {
		// lipgloss reads the terminal itself, so --no-color has to reach it separately.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	var answer string
	field := huh.NewInput().Title(gutter + title).Value(&answer).Prompt(gutter + "> ")
	// The question, the typed line, and the blank row above the key help.
	height := 3
	if description != "" {
		field, height = field.Description(gutter+description), height+1
	}
	// A form left to size itself gives the group no room for the field, which draws the question
	// as blank lines with only the key help under them.
	form := huh.NewForm(huh.NewGroup(field)).WithTheme(askTheme(t)).WithOutput(p.Stderr).WithInput(in).WithWidth(p.width()).WithHeight(height)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrPickCancelled
		}
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

// askTheme is the picker's theme with the typed line added: the prompt in cyan, like the pick
// arrow it stands in for, and the text itself in the terminal's own colour.
func askTheme(t Theme) *huh.Theme {
	h := pickTheme(t)
	plain := lipgloss.NewStyle()
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.TextInput.Prompt, f.TextInput.Text = plain, plain
		if t.Color {
			f.TextInput.Prompt = plain.Foreground(lipgloss.Color("6"))
		}
	}
	return h
}
