package out

import (
	"errors"
	"io"

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
	if !t.Color {
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
