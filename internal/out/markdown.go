package out

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/muesli/termenv"
)

const markdownColumns = 80

// Markdown renders a page in the theme's palette: the gutter as the document margin, headings
// bold, code spans in the command colour, secondary text grey, no backgrounds and no blue, wrapped
// at the terminal width or 80 columns. Without colour the markdown prints as it is.
func (l *Lines) Markdown(text string) {
	t := l.T
	if !t.HasColor {
		fmt.Fprint(l.W, text)
		return
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(t.markdownStyle()),
		// Only a coloured theme gets here, and glamour v1 still takes termenv's profile.
		glamour.WithColorProfile(termenv.ANSI256),
		glamour.WithChromaFormatter("terminal16"),
		glamour.WithWordWrap(min(TerminalWidth(l.W), markdownColumns)),
	)
	if err != nil {
		fmt.Fprint(l.W, text)
		return
	}
	rendered, err := r.Render(text)
	if err != nil {
		fmt.Fprint(l.W, text)
		return
	}
	// glamour pads every line out to the wrap width for the sake of backgrounds, which this style has none of.
	for line := range strings.SplitSeq(strings.TrimRight(rendered, "\n"), "\n") {
		fmt.Fprintln(l.W, strings.TrimRight(line, " "))
	}
}

func (t Theme) markdownStyle() ansi.StyleConfig {
	grey := strconv.Itoa(t.GreyIndex)
	colour := func(c string) ansi.StylePrimitive { return ansi.StylePrimitive{Color: ptr(c)} }
	prefix := func(p string) ansi.StyleBlock { return ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: p}} }
	return ansi.StyleConfig{
		Document:       ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{BlockSuffix: "\n"}, Margin: ptr(uint(len(gutter)))},
		BlockQuote:     ansi.StyleBlock{Indent: ptr(uint(1)), IndentToken: ptr(t.GlyphBar() + " ")},
		List:           ansi.StyleList{LevelIndent: 2},
		Heading:        ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{BlockSuffix: "\n", Bold: ptr(true)}},
		H2:             prefix("## "),
		H3:             prefix("### "),
		H4:             prefix("#### "),
		H5:             prefix("##### "),
		H6:             prefix("###### "),
		Strikethrough:  ansi.StylePrimitive{CrossedOut: ptr(true)},
		Emph:           ansi.StylePrimitive{Italic: ptr(true)},
		Strong:         ansi.StylePrimitive{Bold: ptr(true)},
		HorizontalRule: ansi.StylePrimitive{Color: ptr(grey), Format: "\n--------\n"},
		Item:           ansi.StylePrimitive{BlockPrefix: t.GlyphDot() + " "},
		Enumeration:    ansi.StylePrimitive{BlockPrefix: ". "},
		Task:           ansi.StyleTask{Ticked: "[" + t.GlyphOK() + "] ", Unticked: "[ ] "},
		Link:           colour(grey),
		Image:          colour(grey),
		ImageText:      ansi.StylePrimitive{Color: ptr(grey), Format: "Image: {{.text}}"},
		Code:           ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: ptr("6"), Bold: ptr(true)}},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{Margin: ptr(uint(len(gutter)))},
			// chroma takes RGB, and its #ansi names are the ones the terminal16 formatter maps
			// back onto the terminal's own 16 colours: teal is cyan, brown yellow.
			Chroma: &ansi.Chroma{
				Error:           colour("#ansidarkred"),
				Comment:         colour("#ansidarkgray"),
				CommentPreproc:  colour("#ansidarkgray"),
				Keyword:         colour("#ansiteal"),
				KeywordReserved: colour("#ansiteal"),
				KeywordType:     colour("#ansiteal"),
				NameBuiltin:     colour("#ansiteal"),
				NameFunction:    colour("#ansiteal"),
				LiteralNumber:   colour("#ansibrown"),
				LiteralString:   colour("#ansidarkgreen"),
				GenericDeleted:  colour("#ansidarkred"),
				GenericInserted: colour("#ansidarkgreen"),
				GenericEmph:     ansi.StylePrimitive{Italic: ptr(true)},
				GenericStrong:   ansi.StylePrimitive{Bold: ptr(true)},
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }
