package docs

import (
	"slices"
	"strings"
	"testing"
)

func mustPages(t *testing.T) []*Page {
	t.Helper()
	pages, err := Pages()
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

func describe(r Result) string {
	switch {
	case r.Page != nil:
		return "page " + r.Page.Name
	case r.Section != nil:
		return "section " + r.Section.Page.Name + ": " + r.Section.Heading
	case len(r.Matches) > 0:
		return "matches"
	case len(r.Hits) > 0:
		return "hits"
	}
	return "none"
}

func TestLookup(t *testing.T) {
	pages := mustPages(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"lock"}, "page lock"},
		{[]string{"LOCK"}, "page lock"},
		{[]string{"add"}, "section cli: shulker add"},
		{[]string{"shulker", "add"}, "section cli: shulker add"},
		{[]string{"cli", "lock"}, "section cli: shulker lock"},
		{[]string{"feature", "off"}, "section cli: shulker feature on|off"},
		{[]string{"link", "prism"}, "section cli: shulker link prism"},
		{[]string{"sides"}, "section concepts: Sides"},
		{[]string{"manifest", "pack"}, "section manifest: pack"},
		{[]string{"Edits in the build directory"}, "section concepts: Edits in the build directory"},
		{[]string{"link"}, "matches"},
		{[]string{"pack"}, "matches"},
		{[]string{"build", "directory"}, "hits"},
		{[]string{"qqqq-nothing-matches"}, "none"},
	}
	for _, c := range cases {
		if got := describe(Lookup(pages, c.args)); got != c.want {
			t.Errorf("Lookup(%q) = %s, want %s", c.args, got, c.want)
		}
	}
}

func TestLookupListsEveryPlaceANameIsDocumented(t *testing.T) {
	pages := mustPages(t)
	var got []string
	for _, s := range Lookup(pages, []string{"pack"}).Matches {
		got = append(got, s.Page.Name+": "+s.Heading)
	}
	for _, want := range []string{"cli: shulker pack add", "cli: shulker pack list", "manifest: pack", "lock: pack"} {
		if !slices.Contains(got, want) {
			t.Errorf("matches for pack %q lack %q", got, want)
		}
	}
}

func TestCommandForResolvesBackToItsSection(t *testing.T) {
	pages := mustPages(t)
	for _, p := range pages {
		for _, s := range p.Sections {
			command := CommandFor(pages, s)
			rest, ok := strings.CutPrefix(command, "shulker docs ")
			if !ok {
				t.Fatalf("CommandFor(%s: %s) = %q", p.Name, s.Heading, command)
			}
			args := strings.Fields(rest)
			if before, quoted, ok := strings.Cut(rest, `"`); ok {
				args = append(strings.Fields(before), strings.TrimSuffix(quoted, `"`))
			}
			r := Lookup(pages, args)
			if r.Section != s && !slices.Contains(r.Matches, s) {
				t.Errorf("%q resolves to %s, not %s: %s", command, describe(r), p.Name, s.Heading)
			}
		}
	}
}

func TestPagesAreCleaned(t *testing.T) {
	pages := mustPages(t)
	for _, p := range pages {
		if p.Title == "" || p.Description == "" || strings.HasPrefix(p.Description, `"`) {
			t.Errorf("%s: title %q, description %q", p.Name, p.Title, p.Description)
		}
		for _, raw := range []string{"<br>", "&lt;", "&#123;", "editLink:", "](/", "](#"} {
			if strings.Contains(p.Markdown, raw) {
				t.Errorf("%s still contains %q", p.Name, raw)
			}
		}
		if strings.HasPrefix(p.Markdown, ":::") || strings.Contains(p.Markdown, "\n:::") {
			t.Errorf("%s still contains a ::: container line", p.Name)
		}
	}
	cli := Lookup(pages, []string{"cli"}).Page
	if !strings.Contains(cli.Markdown, "](https://shulker.sh/docs/cli#shulker-init)") {
		t.Error("cli anchor links are not absolute")
	}
}

func TestSearchFindsSectionAndLine(t *testing.T) {
	pages := mustPages(t)
	hits := Search(pages, "BUILD   directory")
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	for _, h := range hits {
		if !strings.Contains(strings.ToLower(h.Text), "build directory") || h.Page.lines[h.Line-1] != h.Text {
			t.Errorf("hit %s:%d %q", h.Page.Name, h.Line, h.Text)
		}
	}
	if len(Search(pages, "   ")) != 0 {
		t.Error("a blank phrase matched")
	}
}
