package cli

import (
	"regexp"
	"strings"
)

var tableHeader = regexp.MustCompile(`\S+(?: \S+)*`)

// tableRows reads a Lines.Table back out of stdout: one map per row keyed by header, the first
// column under "", with a folded cell's lines joined so a wrapped path or message reads whole.
// Cells fold at spaces as well as separators, so compare them with the spaces removed.
func tableRows(stdout string) []map[string]string {
	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		if i+1 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "─") {
			continue
		}
		var names []string
		var starts []int
		for _, m := range tableHeader.FindAllStringIndex(line, -1) {
			names, starts = append(names, line[m[0]:m[1]]), append(starts, m[0])
		}
		names, starts = append([]string{""}, names...), append([]int{0}, starts...)
		var rows []map[string]string
		for _, body := range lines[i+2:] {
			if strings.TrimSpace(body) == "" {
				break
			}
			r := []rune(body)
			cell := func(col int) string {
				end := len(r)
				if col+1 < len(starts) {
					end = min(end, starts[col+1])
				}
				return strings.TrimSpace(string(r[min(starts[col], len(r)):end]))
			}
			if cell(1) != "" {
				rows = append(rows, map[string]string{})
			}
			for col, name := range names {
				rows[len(rows)-1][name] += cell(col)
			}
		}
		return rows
	}
	return nil
}

// squash drops every space and line break, so a cell that folded at a space compares whole.
func squash(s string) string { return strings.Join(strings.Fields(s), "") }
