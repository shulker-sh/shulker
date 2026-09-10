package near

import (
	"sort"
	"strings"
)

func EditDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func Closest(want string, candidates []string, limit int) []string {
	type scored struct {
		name string
		dist int
	}
	max := min(len(want)/3+1, 3)
	var hits []scored
	for _, c := range candidates {
		if d := EditDistance(strings.ToLower(want), strings.ToLower(c)); d <= max {
			hits = append(hits, scored{c, d})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].dist != hits[j].dist {
			return hits[i].dist < hits[j].dist
		}
		return hits[i].name < hits[j].name
	})
	var names []string
	for i, h := range hits {
		if i == limit {
			break
		}
		names = append(names, h.name)
	}
	return names
}
