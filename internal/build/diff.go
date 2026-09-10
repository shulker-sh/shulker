package build

import (
	"bytes"
	"fmt"
	"strings"
)

const diffContext = 3

type edit struct {
	kind byte
	line string
}

func unifiedDiff(name string, a, b []byte) string {
	if bytes.Equal(a, b) {
		return ""
	}
	if isBinary(a) || isBinary(b) {
		return fmt.Sprintf("Binary files a/%s and b/%s differ\n", name, name)
	}
	edits := diffLines(splitLines(a), splitLines(b))
	var buf strings.Builder
	fmt.Fprintf(&buf, "--- a/%s\n+++ b/%s\n", name, name)
	for _, h := range hunks(edits) {
		buf.WriteString(h)
	}
	return buf.String()
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := string(data)
	trailing := strings.HasSuffix(s, "\n")
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if !trailing {
		lines[len(lines)-1] += "\n\\ No newline at end of file"
	}
	return lines
}

func diffLines(a, b []string) []edit {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}
	v := make([]int, 2*max+2)
	var trace [][]int
	offset := max
	for d := 0; d <= max; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b, offset)
			}
		}
	}
	return nil
}

func backtrack(trace [][]int, a, b []string, offset int) []edit {
	x, y := len(a), len(b)
	var out []edit
	for d := len(trace) - 1; d >= 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			out = append(out, edit{' ', a[x]})
		}
		if d == 0 {
			break
		}
		if x == prevX {
			y--
			out = append(out, edit{'+', b[y]})
		} else {
			x--
			out = append(out, edit{'-', a[x]})
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func hunks(edits []edit) []string {
	var out []string
	i := 0
	for i < len(edits) {
		if edits[i].kind == ' ' {
			i++
			continue
		}
		start := max(i-diffContext, 0)
		last := i
		for j := i + 1; j < len(edits); j++ {
			if edits[j].kind != ' ' {
				last = j
			} else if j-last > 2*diffContext {
				break
			}
		}
		end := min(last+1+diffContext, len(edits))
		out = append(out, formatHunk(edits, start, end))
		i = end
	}
	return out
}

func formatHunk(edits []edit, start, end int) string {
	aStart, bStart := 1, 1
	for _, e := range edits[:start] {
		if e.kind != '+' {
			aStart++
		}
		if e.kind != '-' {
			bStart++
		}
	}
	var aLen, bLen int
	var body strings.Builder
	for _, e := range edits[start:end] {
		if e.kind != '+' {
			aLen++
		}
		if e.kind != '-' {
			bLen++
		}
		body.WriteByte(e.kind)
		body.WriteString(e.line)
		body.WriteByte('\n')
	}
	return fmt.Sprintf("@@ -%s +%s @@\n%s", hunkRange(aStart, aLen), hunkRange(bStart, bLen), body.String())
}

func hunkRange(start, length int) string {
	if length == 1 {
		return fmt.Sprint(start)
	}
	if length == 0 {
		return fmt.Sprintf("%d,0", start-1)
	}
	return fmt.Sprintf("%d,%d", start, length)
}
