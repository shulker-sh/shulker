package mcver

import "testing"

func TestOrder(t *testing.T) {
	ordered := []string{"1.21.1", "24w33a", "26.1", "26.1.1-rc-1", "26.1.1", "26.2", "26.3-snapshot-1", "26.3-snapshot-10", "26.3-pre-1", "26.3-pre-3", "26.3-rc-1", "26.3"}
	for i := 1; i < len(ordered); i++ {
		a, b := MustParse(ordered[i-1]), MustParse(ordered[i])
		if a.ID == "24w33a" || b.ID == "24w33a" {
			continue
		}
		if Compare(a, b) >= 0 {
			t.Errorf("%s should sort before %s", a, b)
		}
	}
}

func TestLoaderPrerelease(t *testing.T) {
	if Compare(MustParse("0.17.0-beta.1"), MustParse("0.17.0")) >= 0 {
		t.Error("beta should sort before release")
	}
	if Compare(MustParse("0.17.0-beta.2"), MustParse("0.17.0-beta.10")) >= 0 {
		t.Error("numeric prerelease parts compare numerically")
	}
}

func TestRanges(t *testing.T) {
	cases := []struct {
		rng  string
		id   string
		want bool
	}{
		{"*", "26.2", true},
		{"*", "26.3-pre-1", false},
		{"26.2", "26.2", true},
		{"26.2", "26.2.1", false},
		{"~26.2", "26.2.1", true},
		{"~26.2", "26.3", false},
		{"~26.2", "26.3-pre-1", false},
		{"^26.1", "26.2", true},
		{"^26.1", "27.1", false},
		{"^0.17", "0.17.3", true},
		{"^0.17", "0.18.0", false},
		{">=26.1 <26.3", "26.2", true},
		{">=26.1 <26.3", "26.3", false},
		{"26.1 || 26.2", "26.2", true},
		{"26.3-pre-1", "26.3-pre-1", true},
		{">=26.3-pre-1", "26.3-pre-3", true},
		{">=26.3-pre-1", "26.3", true},
		{">=26.3-pre-1", "26.4-pre-1", false},
	}
	for _, tc := range cases {
		r, err := ParseRange(tc.rng)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Matches(MustParse(tc.id)); got != tc.want {
			t.Errorf("%q matches %q: got %v want %v", tc.rng, tc.id, got, tc.want)
		}
	}
}

func TestNewest(t *testing.T) {
	var vs []Version
	for _, id := range []string{"26.1", "26.2.1", "26.2", "26.3-pre-1", "26.3"} {
		vs = append(vs, MustParse(id))
	}
	r, _ := ParseRange("~26.2")
	v, ok := Newest(vs, r)
	if !ok || v.ID != "26.2.1" {
		t.Fatalf("got %v %v", v, ok)
	}
	if _, ok := Newest(vs, mustRange("~25.1")); ok {
		t.Fatal("expected no match")
	}
}

func mustRange(s string) Range {
	r, err := ParseRange(s)
	if err != nil {
		panic(err)
	}
	return r
}
