package out

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	barWidth     = 20
	nameCap      = 48
	frameEvery   = 80 * time.Millisecond
	widestBytes  = "999.9 MB"
	spinnerASCII = `|/-\`
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	longest int
	drawn   []int
	frame   int
	start   time.Time
	stop    chan struct{}
	stopped chan struct{}
}

// Download names one file the bar will cover; Size is 0 when unknown.
type Download struct {
	Name string
	Size int64
}

// Progress starts a bar for the files. It fills by bytes when every size is
// known and by file count otherwise; the longest name sizes the name column.
func (p *Printer) Progress(verb string, files []Download) *Progress {
	if p.JSON {
		return nil
	}
	p.Settle()
	pr := &Progress{l: p.Err(), verb: verb, total: len(files), sizes: map[string]int64{}, start: time.Now()}
	known := true
	for _, f := range files {
		pr.longest = max(pr.longest, len([]rune(f.Name)))
		pr.sizes[f.Name] = f.Size
		pr.totalBy += f.Size
		known = known && f.Size > 0
	}
	if !known {
		pr.totalBy = 0
	}
	if f, ok := p.Stderr.(*os.File); ok && isTerminal(f) {
		pr.tty = f
		pr.stop, pr.stopped = make(chan struct{}), make(chan struct{})
		go pr.spin()
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

// Finish clears the bar and prints the ok line with the byte count and time.
func (pr *Progress) Finish() {
	if pr == nil {
		return
	}
	pr.halt()
	elapsed := time.Since(pr.start)
	noun := "files"
	if pr.done == 1 {
		noun = "file"
	}
	pr.l.OK(fmt.Sprintf("%s %d %s", pastTense(pr.verb), pr.done, noun), fmt.Sprintf("%s in %.1fs", humanBytes(pr.bytes), elapsed.Seconds()))
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
	close(pr.stop)
	<-pr.stopped
	pr.mu.Lock()
	fmt.Fprint(pr.tty, pr.backToTop(terminalWidth(pr.tty)))
	pr.drawn = nil
	pr.mu.Unlock()
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
			pr.frame++
			pr.mu.Unlock()
			pr.redraw()
		}
	}
}

func (pr *Progress) redraw() {
	if pr.tty == nil {
		return
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
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
		return []string{head + " " + t.Grey(clip(name, room, t.Ellipsis()))}
	}
	aside := ""
	if size := pr.sizes[name]; size > 0 {
		aside = t.Aside(humanBytes(size))
	}
	return []string{head, gutter + gutter + t.Grey(clip(name, width-len(gutter)*2-Width(aside), t.Ellipsis())) + aside}
}

// head renders the spinner, verb, bar, and count. Level drops the byte aside
// first and the bar second when the window is too narrow for them.
func (pr *Progress) head(width, level int, widest bool) string {
	t := pr.l.T
	spinner := spinnerFrames[pr.frame%len(spinnerFrames)]
	if t.ASCII {
		spinner = string(spinnerASCII[pr.frame%len(spinnerASCII)])
	}
	line := gutter + t.paint(spinner, sgrCyan, sgrBold) + " " + pr.verb + " "
	if level == 0 || level == 1 {
		line += pr.bar() + " "
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

func (pr *Progress) bar() string {
	t := pr.l.T
	full, tip, empty := "━", "╸", "─"
	if t.ASCII {
		full, tip, empty = "=", ">", "-"
	}
	n := 0
	switch {
	case pr.totalBy > 0:
		n = int(int64(barWidth) * min(pr.bytes, pr.totalBy) / pr.totalBy)
	case pr.total > 0:
		n = barWidth * pr.done / pr.total
	}
	filled := strings.Repeat(full, n)
	if n < barWidth {
		filled += tip
	}
	return t.Cyan(filled) + t.Grey(strings.Repeat(empty, max(0, barWidth-n-1)))
}

func clip(s string, room int, ellipsis string) string {
	r := []rune(s)
	if room < 1 {
		return ""
	}
	if len(r) <= room {
		return s
	}
	keep := max(0, room-len([]rune(ellipsis)))
	return string(r[:keep]) + ellipsis
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
	}
	if stem, ok := strings.CutSuffix(verb, "ying"); ok {
		return stem + "ied"
	}
	return strings.TrimSuffix(verb, "ing") + "ed"
}
