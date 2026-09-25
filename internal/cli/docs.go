package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/docs"
	"shulker.sh/shulker/internal/out"
)

const (
	docsExcerptWidth = 100
	docsExcerptLines = 20
)

type docsPageInfo struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type docsText struct {
	Page     string `json:"page"`
	Heading  string `json:"heading"`
	Markdown string `json:"markdown"`
}

type docsMatch struct {
	Page    string `json:"page"`
	Heading string `json:"heading"`
	Command string `json:"command"`
	Line    int    `json:"line,omitempty"`
	Text    string `json:"text,omitempty"`
}

func (a *app) docsCmd() *cobra.Command {
	var search bool
	cmd := &cobra.Command{
		Use:         "docs [page|command|section]...",
		Annotations: reads(),
		Short:       "Print shulker's documentation",
		RunE: func(_ *cobra.Command, args []string) error {
			pages, err := docs.Pages()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			switch {
			case search && strings.TrimSpace(query) == "":
				return out.Errorf("usage", "--search needs a phrase to search for")
			case search:
				return a.emitDocsSearch(pages, query, docs.Search(pages, query))
			case len(args) == 0:
				return a.emitDocsIndex(pages)
			}
			r := docs.Lookup(pages, args)
			switch {
			case r.Page != nil:
				return a.emitDocsText(docsText{Page: r.Page.Name, Heading: r.Page.Title, Markdown: r.Page.Markdown})
			case r.Section != nil:
				return a.emitDocsText(docsText{Page: r.Section.Page.Name, Heading: r.Section.Heading, Markdown: r.Section.Markdown})
			case len(r.Matches) > 0:
				return a.emitDocsMatches(pages, query, r.Matches)
			case len(r.Hits) > 0:
				return a.emitDocsSearch(pages, query, r.Hits)
			}
			e := out.Errorf("topic-not-found", "nothing in the docs matches %q", query)
			for _, p := range pages {
				e.Candidates = append(e.Candidates, p.Name)
			}
			if len(args) == 1 {
				e.Given = args[0]
			}
			return e
		},
	}
	cmd.Flags().BoolVarP(&search, "search", "s", false, "search every page for the words instead of looking up a page or heading")
	return cmd
}

func (a *app) emitDocsIndex(pages []*docs.Page) error {
	infos := make([]docsPageInfo, len(pages))
	for i, p := range pages {
		infos[i] = docsPageInfo{Name: p.Name, Title: p.Title, Description: p.Description}
	}
	data := struct {
		Pages []docsPageInfo `json:"pages"`
	}{infos}
	return a.printer.Emit(data, func(l *out.Lines) {
		items := make([]out.Item, len(pages))
		for i, p := range pages {
			items[i] = out.Item{Kind: out.Note, Name: l.T.Command(p.Name), Text: p.Description}
		}
		l.Items(items...)
		l.Nudge("Print a page, a command, or a section", "shulker docs add")
	})
}

func (a *app) emitDocsText(text docsText) error {
	return a.printer.Emit(text, func(l *out.Lines) { l.Markdown(text.Markdown) })
}

func (a *app) emitDocsMatches(pages []*docs.Page, query string, sections []*docs.Section) error {
	matches := make([]docsMatch, len(sections))
	for i, s := range sections {
		matches[i] = docsMatch{Page: s.Page.Name, Heading: s.Heading, Command: docs.CommandFor(pages, s)}
	}
	data := struct {
		Matches []docsMatch `json:"matches"`
	}{matches}
	return a.printer.Emit(data, func(l *out.Lines) {
		t := l.T
		l.Info(fmt.Sprintf("%s matches %s", query, plural(len(matches), "section", "sections")))
		width := 0
		for _, m := range matches {
			width = max(width, out.Width(m.Command))
		}
		for i, m := range matches {
			aside := m.Page
			if parent := sections[i].Parent; parent != "" {
				aside += ": " + parent
			}
			l.Plain(t.Grey(t.GlyphDot()) + " " + t.Command(m.Command) + strings.Repeat(" ", width-out.Width(m.Command)) + t.Aside(aside))
		}
		l.Nudge("Search every page", "shulker docs --search "+query)
	})
}

func (a *app) emitDocsSearch(pages []*docs.Page, query string, hits []docs.Hit) error {
	type group struct {
		command string
		hits    []int
	}
	var groups []*group
	byPlace := map[[2]any]*group{}
	matches := make([]docsMatch, len(hits))
	for i, h := range hits {
		place := [2]any{h.Page, h.Section}
		g := byPlace[place]
		if g == nil {
			g = &group{command: "shulker docs " + h.Page.Name}
			if h.Section != nil {
				g.command = docs.CommandFor(pages, h.Section)
			}
			byPlace[place] = g
			groups = append(groups, g)
		}
		g.hits = append(g.hits, i)
		heading := h.Page.Title
		if h.Section != nil {
			heading = h.Section.Heading
		}
		matches[i] = docsMatch{Page: h.Page.Name, Heading: heading, Command: g.command, Line: h.Line, Text: h.Text}
	}
	data := struct {
		Query   string      `json:"query"`
		Matches []docsMatch `json:"matches"`
	}{query, matches}
	return a.printer.Emit(data, func(l *out.Lines) {
		t := l.T
		if len(hits) == 0 {
			l.Info(fmt.Sprintf("no lines match %q", query))
			return
		}
		l.Info(fmt.Sprintf("%s match %q in %s", plural(len(hits), "line", "lines"), query, plural(len(groups), "section", "sections")))
		l.Blank()
		shown := 0
		for i, g := range groups {
			if shown >= docsExcerptLines {
				word := "sections"
				if len(groups)-i == 1 {
					word = "section"
				}
				l.Muted(fmt.Sprintf("%d more %s; use a longer phrase, or pass --json for every match", len(groups)-i, word))
				break
			}
			l.Plain(t.Grey(t.GlyphDot()) + " " + t.Command(g.command))
			for _, hit := range g.hits {
				if shown == docsExcerptLines {
					break
				}
				l.Plain("    " + excerpt(t, hits[hit].Text, query))
				shown++
			}
		}
	})
}

// excerpt trims a line to about docsExcerptWidth runes around the first match
// of the phrase, which it bolds.
func excerpt(t out.Theme, line, phrase string) string {
	runes := []rune(strings.TrimSpace(docs.PlainLinks(line)))
	lower := []rune(strings.ToLower(string(runes)))
	if len(lower) != len(runes) {
		lower = runes
	}
	q := []rune(strings.ToLower(strings.Join(strings.Fields(phrase), " ")))
	at := -1
	for i := 0; len(q) > 0 && i+len(q) <= len(lower); i++ {
		if string(lower[i:i+len(q)]) == string(q) {
			at = i
			break
		}
	}
	if at < 0 {
		at, q = 0, nil
	}
	start := min(at, max(0, at-(docsExcerptWidth-len(q))/2))
	end := min(len(runes), start+docsExcerptWidth)
	start = min(start, max(0, end-docsExcerptWidth))
	matchEnd := min(at+len(q), end)
	var b strings.Builder
	if start > 0 {
		b.WriteString(t.Ellipsis())
	}
	b.WriteString(string(runes[start:at]))
	b.WriteString(t.Bold(string(runes[at:matchEnd])))
	b.WriteString(string(runes[matchEnd:end]))
	if end < len(runes) {
		b.WriteString(t.Ellipsis())
	}
	return b.String()
}
