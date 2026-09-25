package dotted

import "testing"

func mustParse(t *testing.T, id string) Version {
	t.Helper()
	v, err := Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOrder(t *testing.T) {
	ordered := []string{"0.10.6+build.214", "0.17.3", "0.30.1", "0.30.2-beta.1", "0.31.0-beta.2", "0.31.0-beta.10", "0.31.0", "21.1.250", "26.1.0.0-alpha.9+snapshot-6", "26.1.0.0-alpha.11+snapshot-7", "26.1.0.0", "26.2.0.56-beta", "26.2.0.57", "26.2.0.87", "65.1.3"}
	for i := 1; i < len(ordered); i++ {
		a, b := mustParse(t, ordered[i-1]), mustParse(t, ordered[i])
		if Compare(a, b) >= 0 {
			t.Errorf("%s should sort before %s", a, b)
		}
	}
	if Compare(mustParse(t, "65.1"), mustParse(t, "65.1.0")) != 0 {
		t.Error("missing parts count as zero")
	}
}

func TestParseRejects(t *testing.T) {
	for _, id := range []string{"", "26.2-65.1.3.", "v1.0", "1..2", "beta"} {
		if _, err := Parse(id); err == nil {
			t.Errorf("%q should not parse", id)
		}
	}
}

func TestRanges(t *testing.T) {
	cases := []struct {
		rng  string
		id   string
		want bool
	}{
		{"*", "0.30.1", true},
		{"*", "0.31.0-beta.4", false},
		{"^0.30", "0.30.1", true},
		{"^0.30", "0.30.2-beta.1", false},
		{"^0.30", "0.31.0", false},
		{">=0.31.0-beta", "0.31.0-beta.4", true},
		{">=0.31.0-beta", "0.32.0-beta.1", false},
		{"^26.2.0", "26.2.0.87", true},
		{"^26.2.0", "26.2.0.56-beta", false},
		{"^26.2.0", "27.0.0.1", false},
		{"~26.2", "26.2.0.87", true},
		{"~26.2", "26.3.0.1", false},
		{"^65.1", "65.1.3", true},
		{"^65.1", "66.0.0", false},
		{"~65", "65.9.1", true},
		{"65.1.3", "65.1.3", true},
		{"=65.1", "65.1.3", false},
		{"<0.17 || >=0.19", "0.19.5", true},
		{"<0.17 || >=0.19", "0.18.0", false},
	}
	for _, c := range cases {
		r, err := ParseRange(c.rng)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Matches(mustParse(t, c.id)); got != c.want {
			t.Errorf("%s matches %s = %v, want %v", c.rng, c.id, got, c.want)
		}
	}
}

func TestNewest(t *testing.T) {
	var vs []Version
	for _, id := range []string{"0.20.0-beta.9", "0.30.1", "0.31.0-beta.4", "0.30.0", "0.29.2"} {
		vs = append(vs, mustParse(t, id))
	}
	r, _ := ParseRange("*")
	if v, ok := Newest(vs, r); !ok || v.ID != "0.30.1" {
		t.Errorf("newest = %v %v, want 0.30.1", v, ok)
	}
}
