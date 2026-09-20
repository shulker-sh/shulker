package out

import (
	"errors"
	"io"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ErrPickCancelled is a picker the user escaped out of.
var ErrPickCancelled = errors.New("cancelled")

// Choice is one row of a picker: the line as it reads, already styled, and what choosing it means.
type Choice struct {
	Label string
	Value string
}

// CanPick reports whether a picker can be drawn: it takes over the screen, so the stream it draws
// on has to be a terminal, not a pipe or a test's buffer.
func (p *Printer) CanPick() bool { return !p.JSON && IsTerminal(p.Stderr) }

// pickRows is how many rows show before the list scrolls.
const pickRows = 10

// Pick asks which of several things was meant. It draws on stderr, where every prompt goes, and
// takes its rows already styled, so the picker decides nothing about colour that the theme hasn't.
func (p *Printer) Pick(title string, choices []Choice, in io.Reader) (string, error) {
	t := p.ErrTheme
	if !t.Color {
		// lipgloss reads the terminal itself, so --no-color has to reach it separately.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	options := make([]huh.Option[string], len(choices))
	for i, c := range choices {
		options[i] = huh.NewOption(c.Label, c.Value)
	}
	var chosen string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(gutter + title).
			Options(options...).
			Value(&chosen).
			Height(min(len(choices), pickRows) + 1),
	)).WithTheme(pickTheme(t)).WithOutput(p.Stderr).WithInput(in)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrPickCancelled
		}
		return "", err
	}
	return chosen, nil
}

// pickTheme keeps the picker inside shulker's own vocabulary: the two-space gutter, the ‣ pick
// arrow in cyan, grey help, and no border, background or padding. The rows themselves are left
// unstyled here, because they arrive carrying the theme's own colours and a style wrapped around
// them would end at the first reset inside.
func pickTheme(t Theme) *huh.Theme {
	h := huh.ThemeBase()
	plain := lipgloss.NewStyle()
	cursor := plain.SetString(gutter + t.ArrowPick() + " ")
	if t.Color {
		cursor = cursor.Foreground(lipgloss.Color("6"))
	}
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.Base, f.Card = plain, plain
		f.Title, f.Description = plain, plain
		f.Option, f.SelectedOption, f.UnselectedOption = plain, plain, plain
		f.SelectSelector = cursor
	}
	h.Form.Base, h.Group.Base = plain, plain
	if t.Color {
		grey := lipgloss.Color(strconv.Itoa(t.GreyIndex))
		h.Help.ShortKey = plain.Foreground(grey)
		h.Help.ShortDesc = plain.Foreground(grey)
		h.Help.ShortSeparator = plain.Foreground(grey)
		h.Help.Ellipsis = plain.Foreground(grey)
	} else {
		h.Help.ShortKey, h.Help.ShortDesc = plain, plain
		h.Help.ShortSeparator, h.Help.Ellipsis = plain, plain
	}
	return h
}
