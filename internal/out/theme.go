package out

import (
	"image/color"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

const (
	GreyDark     = 248
	GreyLight    = 242
	GreyFallback = 244
)

// Theme is how one stream renders: whether it takes colour and links, and which grey it uses.
type Theme struct {
	HasColor  bool
	ASCII     bool
	GreyIndex int
	HasLinks  bool
}

type Options struct {
	NoColor bool
	ASCII   bool
}

// Detect builds the theme for each stream. Colour needs a terminal on that
// stream and no NO_COLOR or TERM=dumb; the grey is queried from the terminal
// once when any stream is coloured.
func Detect(stdout, stderr io.Writer, opts Options) (out, err Theme) {
	outTTY, errTTY := IsTerminal(stdout), IsTerminal(stderr)
	color := !opts.NoColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	base := Theme{ASCII: opts.ASCII, GreyIndex: GreyFallback}
	if color && (outTTY || errTTY) {
		base.GreyIndex = queryGrey(stdout, stderr, outTTY)
	}
	out, err = base, base
	out.HasColor, err.HasColor = color && outTTY, color && errTTY
	out.HasLinks, err.HasLinks = supportsHyperlinks(outTTY), supportsHyperlinks(errTTY)
	out.ASCII, err.ASCII = opts.ASCII || legacyCodePage(outTTY), opts.ASCII || legacyCodePage(errTTY)
	return out, err
}

// IsTerminal reports whether w is a terminal, which decides both colour and whether a prompt can
// be drawn at all.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func queryGrey(stdout, stderr io.Writer, outTTY bool) int {
	tty := stderr
	if outTTY {
		tty = stdout
	}
	f, ok := tty.(*os.File)
	if !ok {
		return GreyFallback
	}
	bg, ok := queryBackground(os.Stdin, f)
	if !ok {
		return GreyFallback
	}
	if luminance(bg) > 0.4 {
		return GreyLight
	}
	return GreyDark
}

func supportsHyperlinks(tty bool) bool {
	env := os.Getenv
	if v, ok := os.LookupEnv("FORCE_HYPERLINK"); ok {
		return v != "0" && v != "false"
	}
	if !tty || env("CI") != "" || env("TMUX") != "" || env("STY") != "" {
		return false
	}
	if env("WT_SESSION") != "" {
		return true
	}
	program, programVersion, name := env("TERM_PROGRAM"), env("TERM_PROGRAM_VERSION"), env("TERM")
	switch {
	case program == "ghostty" || name == "xterm-ghostty":
		return true
	case program == "iTerm.app":
		return versionAtLeast(programVersion, 3, 1)
	case program == "WezTerm":
		return true
	case program == "vscode":
		return versionAtLeast(programVersion, 1, 72)
	case program == "Apple_Terminal":
		return false
	case name == "xterm-kitty" || name == "alacritty":
		return true
	}
	if vte, err := strconv.Atoi(env("VTE_VERSION")); err == nil && vte >= 5000 {
		return true
	}
	return false
}

var versionParts = regexp.MustCompile(`\d+`)

func versionAtLeast(v string, want ...int) bool {
	parts := versionParts.FindAllString(v, len(want))
	if len(parts) < len(want) {
		return false
	}
	for i, w := range want {
		n, _ := strconv.Atoi(parts[i])
		if n != w {
			return n > w
		}
	}
	return true
}

const (
	sgrReset  = "\x1b[0m"
	sgrBold   = "\x1b[1m"
	sgrRed    = "\x1b[31m"
	sgrGreen  = "\x1b[32m"
	sgrYellow = "\x1b[33m"
	sgrCyan   = "\x1b[36m"
)

func (t Theme) paint(text string, codes ...string) string {
	if !t.HasColor || text == "" {
		return text
	}
	open := strings.Join(codes, "")
	// A reset inside text, from a styled command say, would end this style early.
	inner := strings.ReplaceAll(strings.TrimSuffix(text, sgrReset), sgrReset, sgrReset+open)
	return open + inner + sgrReset
}

func (t Theme) grey() string { return "\x1b[38;5;" + strconv.Itoa(t.GreyIndex) + "m" }

func (t Theme) Bold(s string) string   { return t.paint(s, sgrBold) }
func (t Theme) Red(s string) string    { return t.paint(s, sgrRed) }
func (t Theme) Green(s string) string  { return t.paint(s, sgrGreen) }
func (t Theme) Yellow(s string) string { return t.paint(s, sgrYellow) }
func (t Theme) Cyan(s string) string   { return t.paint(s, sgrCyan) }
func (t Theme) Grey(s string) string   { return t.paint(s, t.grey()) }
func (t Theme) Command(s string) string {
	return t.paint(s, sgrCyan, sgrBold)
}

func (t Theme) Aside(s string) string {
	if s == "" {
		return ""
	}
	return " " + t.Grey("("+s+")")
}

// Markup turns `command` spans into cyan bold text; the backticks never print.
func (t Theme) Markup(text string) string {
	if !strings.Contains(text, "`") {
		return text
	}
	var b strings.Builder
	for i, part := range strings.Split(text, "`") {
		if i%2 == 1 {
			b.WriteString(t.Command(part))
		} else {
			b.WriteString(part)
		}
	}
	return b.String()
}

func (t Theme) glyph(unicode, ascii string) string {
	if t.ASCII {
		return ascii
	}
	return unicode
}

func (t Theme) GlyphOK() string      { return t.glyph("✔", "*") }
func (t Theme) GlyphError() string   { return t.glyph("✘", "x") }
func (t Theme) GlyphDot() string     { return t.glyph("•", "*") }
func (t Theme) GlyphPending() string { return t.glyph("○", "o") }
func (t Theme) GlyphTee() string     { return t.glyph("├─", "|-") }

// GlyphElbowRound closes a tree: the rounded corner under the last child.
func (t Theme) GlyphElbowRound() string { return t.glyph("╰─", "\\-") }
func (t Theme) GlyphBar() string        { return t.glyph("│", "|") }
func (t Theme) ArrowBump() string       { return t.glyph("⟶", "->") }
func (t Theme) ArrowInto() string       { return t.glyph("»", ">>") }
func (t Theme) ArrowPick() string       { return t.glyph("‣", "*") }
func (t Theme) Ellipsis() string        { return t.glyph("…", "...") }

// Style is the blank lipgloss style a charm surface builds on. A lipgloss style renders its paint
// whatever the stream, so the colour roles below set none without colour, and a surface that paints
// a style itself writes through Profile.
func (t Theme) Style() lipgloss.Style { return lipgloss.NewStyle() }

// Profile is the colour profile a charm surface is downsampled to where it is written: ASCII keeps
// the cursor moves a live line makes and drops the paint.
func (t Theme) Profile() colorprofile.Profile {
	if t.HasColor {
		return colorprofile.ANSI256
	}
	return colorprofile.ASCII
}

func (t Theme) foreground(c color.Color) lipgloss.Style {
	if !t.HasColor {
		return t.Style()
	}
	return t.Style().Foreground(c)
}

// The colour roles as lipgloss styles, for the cells of a Table: the same paint Bold, Grey, Cyan,
// Green, Yellow, Red and Command give a string.
func (t Theme) StyleBold() lipgloss.Style {
	if !t.HasColor {
		return t.Style()
	}
	return t.Style().Bold(true)
}
func (t Theme) StyleGrey() lipgloss.Style    { return t.foreground(lipgloss.ANSIColor(t.GreyIndex)) }
func (t Theme) StyleCyan() lipgloss.Style    { return t.foreground(lipgloss.Cyan) }
func (t Theme) StyleGreen() lipgloss.Style   { return t.foreground(lipgloss.Green) }
func (t Theme) StyleYellow() lipgloss.Style  { return t.foreground(lipgloss.Yellow) }
func (t Theme) StyleRed() lipgloss.Style     { return t.foreground(lipgloss.Red) }
func (t Theme) StyleCommand() lipgloss.Style { return t.StyleCyan().Inherit(t.StyleBold()) }

// Link wraps text in an OSC 8 file:// hyperlink when the terminal follows them.
func (t Theme) Link(text, path string) string {
	if !t.HasLinks || !t.HasColor {
		return text
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return text
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return hyperlink(text, u.String())
}

// LinkURL wraps text in an OSC 8 hyperlink to a page, for one the player has to open themselves.
func (t Theme) LinkURL(text, target string) string {
	if !t.HasLinks || !t.HasColor {
		return text
	}
	return hyperlink(text, target)
}

func hyperlink(text, target string) string {
	return "\x1b]8;;" + target + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// Width counts the columns a rendered line occupies.
func Width(s string) int { return ansi.StringWidth(s) }
