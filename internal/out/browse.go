package out

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// BrowseSource answers a browser as its query changes. Rows and Status each run off the
// drawing loop and may block, and neither is handed the query: a source keeps the one SetQuery
// last gave it, so it can tell when the query a call started on has since moved on.
type BrowseSource interface {
	SetQuery(query string)
	Rows() []Choice
	Status() string
}

// Browse draws a query line over a list that follows it: every change to the query asks src
// for fresh rows and a status line. Nothing in it can be chosen; esc or ctrl-c is the only way
// out, and leaving that way is the normal end, so it returns nil.
func (p *Printer) Browse(title string, src BrowseSource, in io.Reader) error {
	t := p.ErrTheme
	if !t.HasColor {
		// lipgloss reads the terminal itself, so --no-color has to reach it separately.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	q := &browseQuery{src: src}
	// A row that wraps scrolls as two, so each is cut to the width left after the cursor.
	rowWidth := p.width() - len(gutter) - Width(t.ArrowPick()+" ") - 1
	rows := func() []huh.Option[string] {
		choices := src.Rows()
		for i, c := range choices {
			choices[i].Label = ansi.Truncate(c.Label, rowWidth, t.Ellipsis())
		}
		return options(choices)
	}
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "exit"))
	// Enter does nothing anywhere: tab moves between the query and the list, and a binding with no
	// keys matches nothing and drops out of the help, where a disabled one would be re-enabled when
	// huh places the field last in its group.
	keys.Input.Next = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "results"))
	keys.Input.Submit = key.NewBinding()
	keys.Select.Prev = key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "query"))
	keys.Select.Next, keys.Select.Submit = key.NewBinding(), key.NewBinding()
	keys.Select.Filter = key.NewBinding()
	input := huh.NewInput().Title(gutter + title).Accessor(q).Prompt(gutter + "> ")
	list := huh.NewSelect[string]().
		OptionsFunc(rows, &q.text).
		DescriptionFunc(func() string { return gutter + src.Status() }, &q.text).
		Height(pickRows + 1)
	group := huh.NewGroup(input, list)
	// The query and its line, the gap, the status line and the rows, the gap and the key help.
	height := 2 + 1 + 1 + pickRows + 2
	p.open(p.Stderr)
	form := huh.NewForm(group).WithTheme(browseTheme(t)).WithOutput(p.Stderr).WithInput(in).
		WithWidth(p.width()).WithHeight(height).
		WithKeyMap(keys).WithLayout(gutterLayout{quit: keys.Quit, group: group})
	if err := form.Run(); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return err
	}
	return nil
}

// browseQuery is the query line's value. huh reads, sets and hashes it on the drawing loop
// alone, so text needs no lock; the source keeps the copy its rows read from other goroutines.
type browseQuery struct {
	text string
	src  BrowseSource
}

func (q *browseQuery) Get() string { return q.text }

func (q *browseQuery) Set(text string) {
	q.text = text
	q.src.SetQuery(text)
}

// browseTheme is the query line's theme, with huh's loading spinner moved into the gutter: it
// takes the multi-select cursor's style, which no browser otherwise draws.
func browseTheme(t Theme) *huh.Theme {
	h := askTheme(t)
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.MultiSelectSelector = lipgloss.NewStyle().PaddingLeft(len(gutter))
	}
	return h
}

// BrowseMarks is Browse for choosing: the list takes marks, and enter adds what is marked, or
// the row under the cursor when nothing is. A mark outlives the query it was made under, so a
// search can be refined between marks; the status line counts them. Escaping is
// ErrPickCancelled.
func (p *Printer) BrowseMarks(title, description string, src BrowseSource, in io.Reader) ([]string, error) {
	t := p.ErrTheme
	if !t.HasColor {
		// lipgloss reads the terminal itself, so --no-color has to reach it separately.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	m := &browseMarks{}
	q := &markedQuery{browseQuery: browseQuery{src: src}, view: &m.view}
	rowWidth := p.width() - len(gutter) - Width(t.ArrowPick()+" ") - Width("[ ] ") - 1
	rows := func() []huh.Option[string] {
		choices := src.Rows()
		for i, c := range choices {
			choices[i].Label = ansi.Truncate(c.Label, rowWidth, t.Ellipsis())
		}
		m.showing(choices)
		return options(choices)
	}
	status := func() string {
		line := src.Status()
		n := m.count()
		switch {
		case n == 0:
		case line == "":
			line = fmt.Sprintf("%d marked", n)
		default:
			line += fmt.Sprintf(" • %d marked", n)
		}
		return gutter + line
	}
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	keys.Input.Next = key.NewBinding(key.WithKeys("tab", "enter"), key.WithHelp("tab", "results"))
	keys.Input.Submit = key.NewBinding()
	keys.MultiSelect.Prev = key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "query"))
	keys.MultiSelect.Toggle = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "mark"))
	// huh turns Next off and Submit on for the last field in a group, which the list always is.
	keys.MultiSelect.Next = key.NewBinding()
	keys.MultiSelect.Submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "add"))
	keys.MultiSelect.Filter = key.NewBinding()
	keys.MultiSelect.SelectAll, keys.MultiSelect.SelectNone = key.NewBinding(), key.NewBinding()
	input := huh.NewInput().Title(gutter + title).Description(gutter + description).Accessor(q).Prompt(gutter + "> ")
	list := huh.NewMultiSelect[string]().
		Filterable(false).
		Accessor(m).
		OptionsFunc(rows, &m.view.Changes).
		DescriptionFunc(status, &m.view).
		Height(pickRows + 1)
	group := huh.NewGroup(input, list)
	// The query, its description and its line, the gap, the status line and the rows, the gap and
	// the key help.
	height := 3 + 1 + 1 + pickRows + 2
	p.open(p.Stderr)
	form := huh.NewForm(group).WithTheme(marksTheme(t)).WithOutput(p.Stderr).WithInput(in).
		WithWidth(p.width()).WithHeight(height).
		WithKeyMap(keys).WithLayout(gutterLayout{quit: keys.Quit, group: group})
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, ErrPickCancelled
		}
		return nil, err
	}
	if marked := m.Get(); len(marked) > 0 {
		return marked, nil
	}
	if hovered, ok := list.Hovered(); ok {
		return []string{hovered}, nil
	}
	return nil, nil
}

// marksView is what the marking list is bound to, read and written on the drawing loop alone.
// The rows follow Changes, a count of query changes, rather than the text: huh keeps the rows it
// was given per binding and puts them back with the marks they had then, so going back to an
// earlier query would undo every mark since. Counting never repeats a binding, and the source's
// own cache keeps a repeat query free. The status line also follows the number marked.
type marksView struct {
	Changes int
	Marked  int
}

type markedQuery struct {
	browseQuery
	view *marksView
}

func (q *markedQuery) Set(text string) {
	if text != q.text {
		q.view.Changes++
	}
	q.browseQuery.Set(text)
}

// browseMarks is the marking list's value. huh sets it on the drawing loop while rows are built
// on others, so it is locked. huh only knows the rows on screen and sets the value to the marked
// ones among them, so a mark on a row the query no longer returns is kept here instead: it stays
// out of the list, where the cursor would land on it and space would undo it.
type browseMarks struct {
	view marksView

	mu      sync.Mutex
	marked  []string
	onShown map[string]bool
}

func (m *browseMarks) showing(rows []Choice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onShown = map[string]bool{}
	for _, c := range rows {
		m.onShown[c.Value] = true
	}
}

func (m *browseMarks) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.marked)
}

func (m *browseMarks) Get() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.marked)
}

func (m *browseMarks) Set(marked []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := slices.DeleteFunc(slices.Clone(m.marked), func(v string) bool { return m.onShown[v] || slices.Contains(marked, v) })
	m.marked = append(kept, marked...)
	m.view.Marked = len(m.marked)
}

// marksTheme is the browser's theme with a mark column, a tick in the theme's own glyph so
// --ascii reaches it too, and the pick arrow back. A multi-select draws its cursor and its
// loading spinner with the same style, so the gutter is padding: the spinner keeps it and drops
// the arrow.
func marksTheme(t Theme) *huh.Theme {
	h := browseTheme(t)
	plain := lipgloss.NewStyle()
	marked := plain.SetString("[" + t.GlyphOK() + "] ")
	cursor := plain.PaddingLeft(len(gutter)).SetString(t.ArrowPick() + " ")
	if t.HasColor {
		marked = marked.Foreground(lipgloss.Color("2"))
		cursor = cursor.Foreground(lipgloss.Color("6"))
	}
	for _, f := range []*huh.FieldStyles{&h.Focused, &h.Blurred} {
		f.SelectedPrefix, f.UnselectedPrefix = marked, plain.SetString("[ ] ")
		f.MultiSelectSelector = cursor
	}
	return h
}
