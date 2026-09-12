package out

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const (
	GreyDark     = 248
	GreyLight    = 242
	GreyFallback = 244
)

type Theme struct {
	Color     bool
	ASCII     bool
	GreyIndex int
	Links     bool
}

type Options struct {
	NoColor bool
	ASCII   bool
}

// Detect builds the theme for each stream. Colour needs a terminal on that
// stream and no NO_COLOR or TERM=dumb; the grey is queried from the terminal
// once when any stream is coloured.
func Detect(stdout, stderr io.Writer, opts Options) (out, err Theme) {
	outTTY, errTTY := isTerminal(stdout), isTerminal(stderr)
	color := !opts.NoColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	base := Theme{ASCII: opts.ASCII, GreyIndex: GreyFallback}
	if color && (outTTY || errTTY) {
		base.GreyIndex = queryGrey(stdout, stderr, outTTY)
	}
	out, err = base, base
	out.Color, err.Color = color && outTTY, color && errTTY
	out.Links, err.Links = hyperlinks(outTTY), hyperlinks(errTTY)
	return out, err
}

func isTerminal(w io.Writer) bool {
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
	bg, ok := queryBackground(f)
	if !ok {
		return GreyFallback
	}
	if luminance(bg) > 0.4 {
		return GreyLight
	}
	return GreyDark
}

func hyperlinks(tty bool) bool {
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
	if !t.Color || text == "" {
		return text
	}
	return strings.Join(codes, "") + text + sgrReset
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

func (t Theme) GlyphOK() string    { return t.glyph("✔", "*") }
func (t Theme) GlyphError() string { return t.glyph("✘", "x") }
func (t Theme) GlyphDot() string   { return t.glyph("•", "*") }
func (t Theme) GlyphTee() string   { return t.glyph("├─", "|-") }
func (t Theme) GlyphElbow() string { return t.glyph("└─", "\\-") }
func (t Theme) GlyphBar() string   { return t.glyph("│", "|") }
func (t Theme) ArrowBump() string  { return t.glyph("⟶", "->") }
func (t Theme) ArrowInto() string  { return t.glyph("»", ">>") }
func (t Theme) ArrowPick() string  { return t.glyph("‣", "*") }
func (t Theme) Ellipsis() string   { return t.glyph("…", "...") }

// Link wraps text in an OSC 8 file:// hyperlink when the terminal follows them.
func (t Theme) Link(text, path string) string {
	if !t.Links || !t.Color {
		return text
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return text
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return "\x1b]8;;" + u.String() + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m|\x1b\]8;;[^\x1b]*\x1b\\`)

// Width counts the columns a rendered line occupies.
func Width(s string) int {
	return len([]rune(ansiSeq.ReplaceAllString(s, "")))
}
