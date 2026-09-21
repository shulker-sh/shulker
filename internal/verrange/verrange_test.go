package verrange

import (
	"strconv"
	"strings"
	"testing"
)

// num is a version of one number, a "-pre" suffix marking a prerelease of it.
type num struct {
	n   int
	pre bool
}

func parseNum(s string) (num, error) {
	core, pre := strings.CutSuffix(s, "-pre")
	n, err := strconv.Atoi(core)
	return num{n, pre}, err
}

func (v num) Compare(o num) int {
	switch {
	case v.n != o.n:
		return v.n - o.n
	case v.pre == o.pre:
		return 0
	case v.pre:
		return -1
	}
	return 1
}

func (v num) IsRelease() bool     { return !v.pre }
func (v num) SameCore(o num) bool { return v.n == o.n }
func (v num) TildeUpper() num     { return num{v.n + 10, true} }
func (v num) CaretUpper() num     { return num{v.n + 100, true} }

func mustParse(t *testing.T, raw string) Range[num] {
	t.Helper()
	r, err := Parse(raw, parseNum)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRangeGrammar(t *testing.T) {
	for _, tc := range []struct {
		raw string
		in  []int
		out []int
	}{
		{"*", []int{1, 500}, nil},
		{">=5 <8", []int{5, 7}, []int{4, 8}},
		{">5 <=8", []int{6, 8}, []int{5, 9}},
		{"=5", []int{5}, []int{6}},
		{"5", []int{5}, []int{6}},
		{"~5", []int{5, 14}, []int{4, 15}},
		{"^5", []int{5, 104}, []int{4, 105}},
		{"<3 || >9", []int{2, 10}, []int{3, 9}},
	} {
		r := mustParse(t, tc.raw)
		for _, n := range tc.in {
			if !r.Matches(num{n: n}) {
				t.Errorf("%q should match %d", tc.raw, n)
			}
		}
		for _, n := range tc.out {
			if r.Matches(num{n: n}) {
				t.Errorf("%q should not match %d", tc.raw, n)
			}
		}
	}
	if _, err := Parse("x", parseNum); err == nil {
		t.Error("a version that doesn't parse must fail")
	}
	if _, err := Parse("1 ||", parseNum); err == nil {
		t.Error("an empty alternative must fail")
	}
}

func TestAPrereleaseMatchesOnlyWhenTheSetNamesItsCore(t *testing.T) {
	if mustParse(t, ">=4").Matches(num{5, true}) {
		t.Error("a prerelease of a core the set doesn't name")
	}
	if !mustParse(t, ">=5-pre").Matches(num{5, true}) {
		t.Error("a prerelease of the core the set names")
	}
	var any Range[num]
	if any.Matches(num{5, true}) || !any.Matches(num{5, false}) {
		t.Error("the zero range matches releases only")
	}
	if !any.Contains(num{5, true}) {
		t.Error("the zero range contains everything")
	}
}

func TestNewestIsTheHighestMatch(t *testing.T) {
	got, ok := Newest([]num{{3, false}, {9, false}, {7, false}}, mustParse(t, "<8"))
	if !ok || got.n != 7 {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := Newest([]num{{9, false}}, mustParse(t, "<8")); ok {
		t.Fatal("nothing matches")
	}
}

func TestComparePrerelease(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		sign int
	}{
		{"1", "2", -1}, {"2", "10", -1}, {"1", "alpha", -1}, {"alpha", "beta", -1}, {"rc.1", "rc.1", 0}, {"rc", "rc.1", -1},
	} {
		got := ComparePrerelease(tc.a, tc.b)
		if (got < 0 && tc.sign >= 0) || (got > 0 && tc.sign <= 0) || (got == 0 && tc.sign != 0) {
			t.Errorf("%q vs %q: %d", tc.a, tc.b, got)
		}
	}
}
