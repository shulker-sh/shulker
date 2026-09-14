package docs

import (
	"regexp"
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

func TestHelpFor(t *testing.T) {
	add, ok := HelpFor("shulker add")
	if !ok || len(add.Description) != 1 || !strings.HasPrefix(add.Description[0], "Add mods to the manifest") || len(add.Examples) != 3 || add.Examples[0] != "shulker add sodium lithium" || add.More || add.Anchor != "shulker-add" {
		t.Fatalf("add %+v", add)
	}
	if lock, _ := HelpFor("shulker lock"); len(lock.Description) != 2 {
		t.Errorf("lock description %q", lock.Description)
	}
	if sync, _ := HelpFor("shulker sync"); !sync.More {
		t.Error("sync goes on past its example but More is false")
	}
	if off, ok := HelpFor("shulker feature off"); !ok || off.Anchor != "shulker-feature-on-off" {
		t.Errorf("feature off %v %+v", ok, off)
	}
	if _, ok := HelpFor("shulker link"); ok {
		t.Error("link has no section of its own")
	}
}

func TestAnchorsMatchTheSite(t *testing.T) {
	cli := Lookup(mustPages(t), []string{"cli"}).Page
	anchors := map[string]bool{}
	for _, s := range cli.Sections {
		anchors[anchor(s.Heading)] = true
	}
	links := regexp.MustCompile(`/docs/cli#([a-z0-9-]+)\)`).FindAllStringSubmatch(cli.Markdown, -1)
	if len(links) == 0 {
		t.Fatal("cli.md links no anchors")
	}
	for _, m := range links {
		if !anchors[m[1]] {
			t.Errorf("no heading has the anchor %s", m[1])
		}
	}
}

func TestPlainLinks(t *testing.T) {
	got := PlainLinks("see [`shulker pull [file...]`](https://shulker.sh/docs/cli#shulker-pull) and [gh](https://cli.github.com)")
	if got != "see `shulker pull [file...]` and gh" {
		t.Fatalf("PlainLinks = %q", got)
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
