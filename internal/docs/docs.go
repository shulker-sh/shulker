package docs

import (
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"shulker.sh/shulker/site"
)

const siteURL = "https://shulker.sh"

var pageNames = []string{"getting-started", "concepts", "cli", "manifest", "instance"}

var (
	headingLine  = regexp.MustCompile("^(#{1,6}) +(.+?) *$")
	blankRuns    = regexp.MustCompile(`\n{3,}`)
	markdownLink = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\]]*\])*)\]\([^)]*\)`)
	nonSlug      = regexp.MustCompile(`[^a-z0-9]+`)
)

type Page struct {
	Name        string
	Title       string
	Description string
	Markdown    string
	Sections    []*Section
	lines       []string
}

type Section struct {
	Page     *Page
	Heading  string
	Parent   string
	Command  string
	Markdown string
	names    []string
	start    int
	end      int
}

type Hit struct {
	Page    *Page
	Section *Section
	Line    int
	Text    string
}

type Result struct {
	Page    *Page
	Section *Section
	Matches []*Section
	Hits    []Hit
}

var Pages = sync.OnceValues(func() ([]*Page, error) {
	pages := make([]*Page, len(pageNames))
	for i, name := range pageNames {
		raw, err := site.Docs.ReadFile("docs/" + name + ".md")
		if err != nil {
			return nil, err
		}
		pages[i] = parsePage(name, string(raw))
	}
	return pages, nil
})

func parsePage(name, raw string) *Page {
	p := &Page{Name: name}
	body := raw
	if rest, ok := strings.CutPrefix(raw, "---\n"); ok {
		if front, after, ok := strings.Cut(rest, "\n---\n"); ok {
			body = after
			for _, line := range strings.Split(front, "\n") {
				if v, ok := strings.CutPrefix(line, "description: "); ok {
					if unquoted, err := strconv.Unquote(v); err == nil {
						v = unquoted
					}
					p.Description = v
				}
			}
		}
	}
	p.Markdown = clean(name, body)
	p.lines = strings.Split(strings.TrimSuffix(p.Markdown, "\n"), "\n")
	p.parseSections()
	return p
}

func clean(page, body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, ":::") {
			kept = append(kept, line)
		}
	}
	text := strings.ReplaceAll(strings.Join(kept, "\n"), "<br>", "; ")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "](/", "]("+siteURL+"/")
	text = strings.ReplaceAll(text, "](#", "]("+siteURL+"/docs/"+page+"#")
	return strings.TrimSpace(blankRuns.ReplaceAllString(text, "\n\n")) + "\n"
}

func (p *Page) parseSections() {
	var open []*Section
	var levels []int
	closeFrom := func(level, at int) {
		for len(open) > 0 && levels[len(levels)-1] >= level {
			open[len(open)-1].end = at
			open, levels = open[:len(open)-1], levels[:len(levels)-1]
		}
	}
	fence := false
	for i, line := range p.lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		m := headingLine.FindStringSubmatch(line)
		if fence || m == nil {
			continue
		}
		level, text := len(m[1]), strings.ReplaceAll(m[2], "`", "")
		if level == 1 {
			if p.Title == "" {
				p.Title = text
			}
			continue
		}
		closeFrom(level, i)
		s := &Section{Page: p, Heading: text, start: i}
		if len(open) > 0 {
			s.Parent = open[len(open)-1].Heading
		}
		if path, ok := strings.CutPrefix(text, "shulker "); ok {
			s.names = expandAlternatives(path)
		}
		if len(s.names) > 0 {
			s.Command = s.names[0]
		} else {
			s.names = []string{strings.ToLower(text)}
		}
		open, levels = append(open, s), append(levels, level)
		p.Sections = append(p.Sections, s)
	}
	closeFrom(1, len(p.lines))
	for _, s := range p.Sections {
		s.Markdown = strings.TrimRight(strings.Join(p.lines[s.start:s.end], "\n"), "\n") + "\n"
	}
}

// expandAlternatives reads a heading like `shulker feature on|off` as
// documenting both commands.
func expandAlternatives(path string) []string {
	words := strings.Fields(strings.ToLower(path))
	if len(words) == 0 {
		return nil
	}
	prefix := strings.Join(words[:len(words)-1], " ")
	var paths []string
	for _, alt := range strings.Split(words[len(words)-1], "|") {
		paths = append(paths, strings.TrimSpace(prefix+" "+alt))
	}
	return paths
}

// Lookup resolves the words after `shulker docs`: a page name, then a
// command, then any other heading, then the words as a phrase to search for.
// A page name followed by more words looks only inside that page.
func Lookup(pages []*Page, args []string) Result {
	if len(args) == 0 {
		return Result{}
	}
	for _, p := range pages {
		if strings.EqualFold(p.Name, args[0]) {
			if len(args) == 1 {
				return Result{Page: p}
			}
			return lookupIn([]*Page{p}, strings.Join(args[1:], " "))
		}
	}
	return lookupIn(pages, strings.Join(args, " "))
}

func lookupIn(pages []*Page, query string) Result {
	q := strings.TrimPrefix(normalize(query), "shulker ")
	var others []*Section
	for _, p := range pages {
		for _, s := range p.Sections {
			switch {
			case s.Command != "" && slices.Contains(s.names, q):
				return Result{Section: s}
			case s.Command == "" && s.names[0] == q, s.Command != "" && s.documentsUnder(q):
				others = append(others, s)
			}
		}
	}
	switch len(others) {
	case 0:
		return Result{Hits: Search(pages, q)}
	case 1:
		return Result{Section: others[0]}
	}
	return Result{Matches: others}
}

func (s *Section) documentsUnder(parent string) bool {
	for _, name := range s.names {
		if strings.HasPrefix(name, parent+" ") {
			return true
		}
	}
	return false
}

func Search(pages []*Page, phrase string) []Hit {
	q := normalize(phrase)
	if q == "" {
		return nil
	}
	var hits []Hit
	for _, p := range pages {
		for i, line := range p.lines {
			if strings.Contains(strings.ToLower(line), q) {
				hits = append(hits, Hit{Page: p, Section: p.sectionAt(i), Line: i + 1, Text: line})
			}
		}
	}
	return hits
}

func (p *Page) sectionAt(line int) *Section {
	var deepest *Section
	for _, s := range p.Sections {
		if s.start <= line && line < s.end {
			deepest = s
		}
	}
	return deepest
}

// CommandFor is the shortest `shulker docs` command that prints s.
func CommandFor(pages []*Page, s *Section) string {
	key, args := s.Command, strings.Fields(s.Command)
	if key == "" {
		key, args = strings.ToLower(s.Heading), []string{strings.ToLower(s.Heading)}
		if strings.Contains(key, " ") {
			key = `"` + key + `"`
		}
	}
	if Lookup(pages, args).Section == s {
		return "shulker docs " + key
	}
	return "shulker docs " + s.Page.Name + " " + key
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func PlainLinks(markdown string) string {
	return markdownLink.ReplaceAllString(markdown, "$1")
}

type CommandHelp struct {
	Description []string
	Examples    []string
	More        bool
	Anchor      string
}

// HelpFor reads a command's cli.md section into what --help shows: the
// paragraphs before its example block and the example lines. More is set when
// the section goes on past them with anything but a flag table.
func HelpFor(commandPath string) (CommandHelp, bool) {
	pages, err := Pages()
	if err != nil {
		return CommandHelp{}, false
	}
	name := strings.TrimPrefix(commandPath, "shulker ")
	for _, p := range pages {
		if p.Name != "cli" {
			continue
		}
		for _, s := range p.Sections {
			if s.Command != "" && slices.Contains(s.names, name) {
				return s.help(), true
			}
		}
	}
	return CommandHelp{}, false
}

func (s *Section) help() CommandHelp {
	h := CommandHelp{Anchor: anchor(s.Heading)}
	lines := strings.Split(strings.TrimSuffix(s.Markdown, "\n"), "\n")[1:]
	var paragraph []string
	i := 0
	for ; i < len(lines) && !isFence(lines[i]); i++ {
		if line := strings.TrimSpace(lines[i]); line != "" {
			paragraph = append(paragraph, line)
		} else if len(paragraph) > 0 {
			h.Description = append(h.Description, strings.Join(paragraph, " "))
			paragraph = nil
		}
	}
	if len(paragraph) > 0 {
		h.Description = append(h.Description, strings.Join(paragraph, " "))
	}
	if i < len(lines) {
		for i++; i < len(lines) && !isFence(lines[i]); i++ {
			if line := strings.TrimSpace(lines[i]); line != "" {
				h.Examples = append(h.Examples, line)
			}
		}
		i++
	}
	flagTable := false
	for ; i < len(lines); i++ {
		switch line := strings.TrimSpace(lines[i]); {
		case line == "":
			flagTable = false
		case strings.HasPrefix(line, "| Flag |"):
			flagTable = true
		case !flagTable:
			h.More = true
		}
	}
	return h
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

// anchor is the id VitePress gives a heading.
func anchor(heading string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(heading), "-"), "-")
}
