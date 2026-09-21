package out

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
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
// on has to be a terminal, not a pipe or a test's buffer, and the run has to be one that asks.
func (p *Printer) CanPick() bool { return !p.JSON && !p.NoInput && IsTerminal(p.Stderr) }

// pickRows is how many rows show before the list scrolls.
const pickRows = 10

// Pick asks which of several things was meant. It draws on stderr, where every prompt goes, and
// takes its rows already styled, so the picker decides nothing about colour that the theme hasn't.
func (p *Printer) Pick(title string, choices []Choice, in io.Reader) (string, error) {
	t := p.ErrTheme
	if !t.HasColor {
		// lipgloss reads the terminal itself, so --no-color has to reach it separately.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	// huh binds quit to ctrl+c alone, which leaves a picker you can only leave by interrupting.
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	var chosen string
	// The field's own height is not enough: a form left to size itself gives every field the
	// height of its whole content, so a long list prints in full instead of scrolling.
	rows := min(len(choices), pickRows)
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(gutter + title).
			Options(options(choices)...).
			Value(&chosen).
			Height(rows + 1),
	)).WithTheme(pickTheme(t)).WithOutput(p.Stderr).WithInput(in).WithHeight(rows + 3).
		WithKeyMap(keys).WithLayout(gutterLayout{quit: keys.Quit})
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrPickCancelled
		}
		return "", err
	}
	return chosen, nil
}

func options(choices []Choice) []huh.Option[string] {
	opts := make([]huh.Option[string], len(choices))
	for i, c := range choices {
		opts[i] = huh.NewOption(c.Label, c.Value)
	}
	return opts
}

// gutterLayout puts huh's help line in the two-space gutter every other line sits in. huh's own
// Group.View joins the footer outside any container the theme can reach, so the only way to indent
// it is to lay the form out here. Every form is one group with no Validate, so its fields and the
// help line are the whole form: the focused field alone for a picker, or all of group when set.
type gutterLayout struct {
	quit  key.Binding
	group *huh.Group
}

func (l gutterLayout) View(f *huh.Form) string {
	view := f.GetFocusedField().View()
	if l.group != nil {
		view = l.group.Content()
	}
	// The field lists only its own keys, and leaving the picker is the one a stuck player needs.
	help := f.Help().ShortHelpView(append(f.KeyBinds(), l.quit))
	if help == "" {
		return view
	}
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		lines[i] = gutter + line
	}
	return view + "\n\n" + strings.Join(lines, "\n")
}

func (gutterLayout) GroupWidth(_ *huh.Form, _ *huh.Group, w int) int { return w }

// pickTheme keeps the picker inside shulker's own vocabulary: the two-space gutter, the ‣ pick
// arrow in cyan, grey help, and no border, background or padding. The rows themselves are left
// unstyled here, because they arrive carrying the theme's own colours and a style wrapped around
// them would end at the first reset inside.
func pickTheme(t Theme) *huh.Theme {
	h := huh.ThemeBase()
	plain := lipgloss.NewStyle()
	cursor := plain.SetString(gutter + t.ArrowPick() + " ")
	if t.HasColor {
		cursor = cursor.Foreground(lipgloss.Color("6"))
	}
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.Base, f.Card = plain, plain
		f.Title, f.Description = plain, plain
		f.Option, f.SelectedOption, f.UnselectedOption = plain, plain, plain
		f.SelectSelector = cursor
	}
	h.Form.Base, h.Group.Base = plain, plain
	if t.HasColor {
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
