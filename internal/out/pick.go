package out

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	m := newPicker(p.ErrTheme, title, choices)
	p.openPrompt()
	if _, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(p.Stderr)).Run(); err != nil {
		return "", err
	}
	if m.cancelled {
		return "", ErrPickCancelled
	}
	return m.chosen(), nil
}

// picker is a list with a cursor that moves through rows that stay put: the window scrolls one
// row at a time, and only once the cursor would leave it. huh's Select jumps the cursor's row to
// the top as soon as it reaches the last visible one, hiding every row above it.
type picker struct {
	theme     Theme
	title     string
	choices   []Choice
	shown     []int
	cursor    int
	top       int
	query     string
	filtering bool
	done      bool
	cancelled bool
}

func newPicker(t Theme, title string, choices []Choice) *picker {
	m := &picker{theme: t, title: title, choices: choices}
	m.refilter()
	return m
}

func (m *picker) Init() tea.Cmd { return nil }

func (m *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if k.Type == tea.KeyCtrlC {
		m.done, m.cancelled = true, true
		return m, tea.Quit
	}
	if m.filtering {
		m.filter(k)
		return m, nil
	}
	switch k.String() {
	case "esc":
		m.done, m.cancelled = true, true
		return m, tea.Quit
	case "enter":
		if len(m.shown) > 0 {
			m.done = true
			return m, tea.Quit
		}
	case "/":
		m.filtering = true
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		m.move(-m.cursor)
	case "end", "G":
		m.move(len(m.shown) - 1 - m.cursor)
	}
	return m, nil
}

func (m *picker) filter(k tea.KeyMsg) {
	switch k.Type {
	case tea.KeyEsc:
		m.filtering, m.query = false, ""
	case tea.KeyEnter:
		m.filtering = false
		return
	case tea.KeyBackspace:
		if m.query == "" {
			m.filtering = false
			return
		}
		r := []rune(m.query)
		m.query = string(r[:len(r)-1])
	case tea.KeyRunes, tea.KeySpace:
		m.query += string(k.Runes)
	default:
		return
	}
	m.refilter()
}

func (m *picker) refilter() {
	m.shown = m.shown[:0]
	q := strings.ToLower(m.query)
	for i, c := range m.choices {
		if strings.Contains(strings.ToLower(ansi.Strip(c.Label)), q) {
			m.shown = append(m.shown, i)
		}
	}
	m.cursor, m.top = 0, 0
}

// move wraps past either end, like huh's Select did, and scrolls just far enough to keep the
// cursor in the window.
func (m *picker) move(by int) {
	n := len(m.shown)
	if n == 0 {
		return
	}
	m.cursor = ((m.cursor+by)%n + n) % n
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+pickRows {
		m.top = m.cursor - pickRows + 1
	}
}

func (m *picker) chosen() string {
	if len(m.shown) == 0 {
		return ""
	}
	return m.choices[m.shown[m.cursor]].Value
}

func (m *picker) View() string {
	if m.done {
		return ""
	}
	t := m.theme
	var b strings.Builder
	b.WriteString(gutter + m.title)
	if m.filtering || m.query != "" {
		b.WriteString("  " + t.Grey("/") + m.query)
	}
	b.WriteString("\n")
	arrow := t.Cyan(t.ArrowPick()) + " "
	blank := strings.Repeat(" ", Width(t.ArrowPick())+1)
	for i := m.top; i < min(len(m.shown), m.top+pickRows); i++ {
		mark := blank
		if i == m.cursor {
			mark = arrow
		}
		b.WriteString(gutter + mark + m.choices[m.shown[i]].Label + "\n")
	}
	if len(m.shown) == 0 {
		b.WriteString(gutter + blank + t.Grey("no matches") + "\n")
	}
	help := "↑ up • ↓ down • / filter • enter choose • esc cancel"
	if m.filtering {
		help = "enter done • esc clear"
	}
	b.WriteString("\n" + gutter + t.Grey(help))
	return b.String()
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
// help line are the whole form.
type gutterLayout struct {
	quit  key.Binding
	group *huh.Group
}

func (l gutterLayout) View(f *huh.Form) string {
	view := l.group.Content()
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
