package out

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	barWidth    = 20
	nameCap     = 48
	frameEvery  = 80 * time.Millisecond
	widestBytes = "999.9 MB"
)

// Progress draws a download bar on a terminal and settles into one ok line.
// Off a terminal only the final line prints; with --json nothing does.
type Progress struct {
	mu      sync.Mutex
	l       *Lines
	tty     *os.File
	verb    string
	total   int
	done    int
	bytes   int64
	sizes   map[string]int64
	totalBy int64
	current string
	one     string
	many    string
	longest int
	drawn   []int
	// halted is set once the bar stops drawing for good, which can come before Finish.
	halted  bool
	halting sync.Once
	owner   *Printer
	wheel   spinner.Model
	bar     progress.Model
	pending tea.Cmd
	stop    chan struct{}
	stopped chan struct{}
	// firstTime records the group's line as shown, and reports whether no group of the same files
	// and counts showed one before.
	firstTime func(key string) bool
	names     []string
}

// Download names one file the bar will cover; Size is 0 when unknown.
type Download struct {
	Name string
	Size int64
}

// Progress starts a bar for the files. It fills by bytes when every size is
// known and by file count otherwise; the longest name sizes the name column.
// A group of the same files and counts this run already settled, as a command
// run again after a download wait has, settles into no line.
func (p *Printer) Progress(verb string, files []Download) *Progress {
	if p.JSON {
		return nil
	}
	p.Settle()
	p.open(p.Stderr)
	pr := newProgress(p.Err(), verb, files)
	pr.firstTime = func(key string) bool {
		p.steps.mu.Lock()
		defer p.steps.mu.Unlock()
		if slices.Contains(p.steps.groups, key) {
			return false
		}
		p.steps.groups = append(p.steps.groups, key)
		return true
	}
	if f, ok := p.Stderr.(*os.File); ok && IsTerminal(f) {
		pr.tty, pr.owner = f, p
		pr.stop, pr.stopped = make(chan struct{}), make(chan struct{})
		p.steps.mu.Lock()
		p.steps.bar = pr
		p.steps.mu.Unlock()
		go pr.spin()
	}
	return pr
}

func newProgress(l *Lines, verb string, files []Download) *Progress {
	pr := &Progress{l: l, verb: verb, total: len(files), sizes: map[string]int64{}, wheel: newSpinner(l.T), bar: newBar(l.T)}
	known := true
	for _, f := range files {
		pr.names = append(pr.names, f.Name)
		pr.longest = max(pr.longest, Width(f.Name))
		pr.sizes[f.Name] = f.Size
		pr.totalBy += f.Size
		known = known && f.Size > 0
	}
	if !known {
		pr.totalBy = 0
	}
	return pr
}

// Counts names what the bar's summary adds up, for a bar covering something more particular than
// files: a run that fetches libraries and then assets says so on each line.
func (pr *Progress) Counts(one, many string) *Progress {
	if pr != nil {
		pr.one, pr.many = one, many
	}
	return pr
}

func (pr *Progress) Bytes(n int64) {
	if pr == nil {
		return
	}
	pr.mu.Lock()
	pr.bytes += n
	pr.mu.Unlock()
}

func (pr *Progress) File(name string) {
	if pr == nil {
		return
	}
	pr.mu.Lock()
	pr.current = name
	pr.mu.Unlock()
	pr.redraw()
}

func (pr *Progress) Advance() {
	if pr == nil {
		return
	}
	pr.mu.Lock()
	pr.done++
	pr.mu.Unlock()
	pr.redraw()
}

// Finish clears the bar and prints the ok line with the byte count, or clears it alone when
// nothing was fetched.
func (pr *Progress) Finish() {
	if pr == nil {
		return
	}
	pr.halt()
	key := strings.Join(append([]string{pr.verb, pr.many}, pr.names...), "\x00")
	if pr.done == 0 || pr.firstTime != nil && !pr.firstTime(key) {
		return
	}
	noun, one := "files", "file"
	if pr.many != "" {
		noun, one = pr.many, pr.one
	}
	if pr.done == 1 {
		noun = one
	}
	aside := ""
	if pr.bytes > 0 {
		aside = humanBytes(pr.bytes)
	}
	pr.l.OK(fmt.Sprintf("%s %d %s", Sentence(pastTense(pr.verb)), pr.done, noun), aside)
}

// Abort clears the bar without a summary, for the error that follows.
func (pr *Progress) Abort() {
	if pr == nil {
		return
	}
	pr.halt()
}

func (pr *Progress) halt() {
	if pr.tty == nil {
		return
	}
	pr.owner.release(pr)
	pr.halting.Do(func() {
		close(pr.stop)
		<-pr.stopped
		pr.mu.Lock()
		fmt.Fprint(pr.tty, pr.backToTop(terminalWidth(pr.tty)))
		pr.drawn, pr.halted = nil, true
		pr.mu.Unlock()
	})
}

// above runs write with the bar cleared off the terminal, so a line lands at column 0 rather than
// over the bar; the next frame draws the bar again below it.
func (pr *Progress) above(write func() (int, error)) (int, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if !pr.halted {
		fmt.Fprint(pr.tty, pr.backToTop(terminalWidth(pr.tty)))
		pr.drawn = nil
	}
	return write()
}

func newBar(t Theme) progress.Model {
	bar := progress.New(progress.WithSolidFill("6"), progress.WithWidth(barWidth), progress.WithFillCharacters('━', '─'), progress.WithSpringOptions(8, 1), progress.WithColorProfile(t.Profile()))
	bar.EmptyColor = strconv.Itoa(t.GreyIndex)
	bar.PercentageStyle = t.Style().Foreground(t.lipglossGrey())
	if t.ASCII {
		bar.Full, bar.Empty = '#', '-'
	}
	return bar
}

func (pr *Progress) spin() {
	defer close(pr.stopped)
	tick := time.NewTicker(frameEvery)
	defer tick.Stop()
	for {
		select {
		case <-pr.stop:
			return
		case <-tick.C:
			pr.mu.Lock()
			pr.wheel, _ = pr.wheel.Update(spinner.TickMsg{})
			pr.mu.Unlock()
			pr.ease()
			pr.redraw()
		}
	}
}

// ease pumps the bar's spring for one frame of the 80 ms clock. The model animates on its own
// 60 fps clock, and each frame command sleeps one tick of that clock before it returns, so pumping
// until the next redraw is due keeps the spring in real time without a tea.Program.
func (pr *Progress) ease() {
	deadline := time.Now().Add(frameEvery)
	for {
		pr.mu.Lock()
		if target := pr.fraction(); target != pr.bar.Percent() {
			pr.pending = pr.bar.SetPercent(target)
		}
		cmd := pr.pending
		pr.mu.Unlock()
		if cmd == nil || !time.Now().Before(deadline) {
			return
		}
		msg := cmd()
		pr.mu.Lock()
		next, c := pr.bar.Update(msg)
		pr.bar, pr.pending = next.(progress.Model), c
		pr.mu.Unlock()
	}
}

func (pr *Progress) fraction() float64 {
	switch {
	case pr.totalBy > 0:
		return float64(min(pr.bytes, pr.totalBy)) / float64(pr.totalBy)
	case pr.total > 0:
		return float64(pr.done) / float64(pr.total)
	}
	return 0
}

func (pr *Progress) redraw() {
	if pr.tty == nil {
		return
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.halted {
		return
	}
	width := terminalWidth(pr.tty)
	lines := pr.render(width)
	var b strings.Builder
	b.WriteString(pr.backToTop(width))
	pr.drawn = pr.drawn[:0]
	for _, line := range lines {
		b.WriteString(line + "\n")
		pr.drawn = append(pr.drawn, Width(line))
	}
	fmt.Fprint(pr.tty, b.String())
}

// backToTop moves up over the previous frame as the terminal shows it now:
// terminals re-wrap on resize, so each line takes ceil(width / columns) rows.
func (pr *Progress) backToTop(width int) string {
	rows := 0
	for _, n := range pr.drawn {
		rows += max(1, (n+width-1)/width)
	}
	s := "\r\x1b[J"
	if rows > 0 {
		s = "\x1b[" + strconv.Itoa(rows) + "A" + s
	}
	return s
}

func (pr *Progress) render(width int) []string {
	t := pr.l.T
	shown := pr.done
	if shown >= pr.total && pr.total > 0 {
		shown = pr.total
	}
	widest := -1
	for i := range 4 {
		if Width(pr.head(width, i, true)) < width {
			widest = i
			break
		}
	}
	if widest < 0 {
		widest = 3
	}
	head := pr.head(width, widest, false)
	room := width - Width(head) - 2
	need := min(pr.longest, nameCap)
	name := pr.current
	if room >= need {
		return []string{head + " " + t.Grey(ansi.Truncate(name, room, t.Ellipsis()))}
	}
	aside := ""
	if size := pr.sizes[name]; size > 0 {
		aside = t.Aside(humanBytes(size))
	}
	return []string{head, gutter + gutter + t.Grey(ansi.Truncate(name, width-len(gutter)*2-Width(aside), t.Ellipsis())) + aside}
}

// head renders the spinner, verb, bar, and count. Level drops the byte aside
// first and the bar second when the window is too narrow for them.
func (pr *Progress) head(width, level int, widest bool) string {
	t := pr.l.T
	line := gutter + pr.wheel.View() + " " + Sentence(pr.verb) + " "
	if pr.many != "" {
		line += pr.many + " "
	}
	if level == 0 || level == 1 {
		line += pr.bar.View() + " "
	}
	count := fmt.Sprintf("%d/%d", pr.done, pr.total)
	if widest {
		count = fmt.Sprintf("%d/%d", pr.total, pr.total)
	}
	line += t.Bold(count)
	if level == 0 || level == 2 {
		line += t.Aside(pr.aside(widest))
	}
	return line
}

// aside is the byte count, "so far of total" when every size was known.
// widest asks for the longest form the aside can take, for measuring.
func (pr *Progress) aside(widest bool) string {
	if pr.totalBy == 0 {
		if widest {
			return widestBytes
		}
		return humanBytes(pr.bytes)
	}
	if widest {
		return widestBytes + " of " + humanBytes(pr.totalBy)
	}
	return humanBytes(min(pr.bytes, pr.totalBy)) + " of " + humanBytes(pr.totalBy)
}

// HumanBytes prints a size the way the progress lines do, with GB for the sizes
// a cache reaches.
func HumanBytes(n int64) string {
	if n < 1<<30 {
		return humanBytes(n)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

func humanBytes(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

func pastTense(verb string) string {
	switch verb {
	case "fetching":
		return "fetched"
	case "downloading":
		return "downloaded"
	case "keeping":
		return "kept"
	case "reading":
		return "read"
	}
	if stem, ok := strings.CutSuffix(verb, "ying"); ok {
		return stem + "ied"
	}
	return strings.TrimSuffix(verb, "ing") + "ed"
}
