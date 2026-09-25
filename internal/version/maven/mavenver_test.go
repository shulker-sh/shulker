package maven

import "testing"

func assertOrdered(t *testing.T, versions []string) {
	t.Helper()
	for i := range versions {
		for j := i + 1; j < len(versions); j++ {
			a, b := Parse(versions[i]), Parse(versions[j])
			if Compare(a, b) >= 0 || Compare(b, a) <= 0 {
				t.Errorf("want %s < %s", versions[i], versions[j])
			}
		}
	}
}

func TestQualifierOrder(t *testing.T) {
	assertOrdered(t, []string{"1-alpha2snapshot", "1-alpha2", "1-alpha-123", "1-beta-2", "1-beta123", "1-m2", "1-m11", "1-rc", "1-cr2", "1-rc123", "1-SNAPSHOT", "1", "1-sp", "1-sp2", "1-sp123", "1-abc", "1-def", "1-pom-1", "1-1-snapshot", "1-1", "1-2", "1-123"})
}

func TestNumberOrder(t *testing.T) {
	assertOrdered(t, []string{"2.0", "2-1", "2.0.a", "2.0.0.a", "2.0.2", "2.0.123", "2.1.0", "2.1-a", "2.1b", "2.1-c", "2.1-1", "2.1.0.1", "2.2", "2.123", "11.a2", "11.a11", "11.b2", "11.b11", "11.m2", "11.m11", "11", "11.a", "11b", "11c", "11m"})
}

func TestLoaderVersionOrder(t *testing.T) {
	assertOrdered(t, []string{"26.1.2.10-beta", "26.1.2.10", "26.2.0.0-beta", "26.2", "26.2.0.56-beta", "26.2.0.57", "26.2.0.87", "26.2.1"})
}

func TestEqual(t *testing.T) {
	for _, pair := range [][2]string{
		{"1", "1.0.0"}, {"1-a1", "1-alpha-1"}, {"1-ga", "1"}, {"1.final", "1"}, {"1-cr1", "1-rc1"},
		{"1-SNAPSHOT", "1-snapshot"}, {"1.0.0-0.0", "1"}, {"0001.02", "1.2"},
	} {
		if c := Compare(Parse(pair[0]), Parse(pair[1])); c != 0 {
			t.Errorf("%s vs %s = %d, want equal", pair[0], pair[1], c)
		}
	}
}

func TestRange(t *testing.T) {
	cases := []struct {
		spec, version string
		want          bool
	}{
		{"[26.1.2.10-beta,)", "26.2.0.87", true},
		{"[26.1.2.10-beta,)", "26.1.2.9", false},
		{"[26.2,26.3)", "26.2", true},
		{"[26.2,26.3)", "26.2.1", true},
		{"[26.2,26.3)", "26.3", false},
		{"[26.2,26.3)", "26.3-beta", true},
		{"(,1.0]", "1.0", true},
		{"(,1.0)", "1.0", false},
		{"(1.0,)", "1.0", false},
		{"[1.2]", "1.2.0", true},
		{"[1.2]", "1.2.1", false},
		{"[1,2),[3,)", "2.5", false},
		{"[1,2),[3,)", "3.1", true},
		{"[1,2) , [3,4)", "1.5", true},
		{"1.0", "0.1", true},
		{"*", "anything", true},
		{"", "anything", true},
		{"[65,)", "65.1.3", true},
		{"[0.8,)", "mc26.2-0.8.1", false},
	}
	for _, c := range cases {
		r, err := ParseRange(c.spec)
		if err != nil {
			t.Errorf("%s: %v", c.spec, err)
			continue
		}
		if got := r.Contains(Parse(c.version)); got != c.want {
			t.Errorf("%s contains %s = %v, want %v", c.spec, c.version, got, c.want)
		}
	}
	for _, spec := range []string{"[1.0", "(1.0)", "[2,1]", "[1,3),[2,4)", "[1,2),3", "[1,2,3]"} {
		if _, err := ParseRange(spec); err == nil {
			t.Errorf("%s: want an error", spec)
		}
	}
}

func TestParseRangeErrorNamesTheRangeOnce(t *testing.T) {
	for spec, want := range map[string]string{
		"[1.21,1.21.1,1.21.2]": "the range has more than two bounds",
		"[2,1]":                "the range has its bounds reversed",
		"(1.0)":                "the range must be written [1.0]",
		"[1,2),[4,3]":          "the range's set [4,3] has its bounds reversed",
		"[1.0":                 "the range is unbounded",
		"[1,3),[2,4)":          "the range has overlapping sets",
		"[1,2),3":              "the range mixes a bare version with sets",
	} {
		_, err := ParseRange(spec)
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", spec, err, want)
		}
	}
}
