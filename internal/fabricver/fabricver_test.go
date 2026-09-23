package fabricver

import "testing"

func TestParse(t *testing.T) {
	for _, s := range []string{"0.3.5", "0.3.5-beta.2", "0.3.5-alpha.6+build.120", "0.3.5+build.3000", "1.0.0-0.3.7", "1.0.0-x.7.z.92", "1.0.0+20130313144700", "1.0.0-beta+exp.sha.5114f85", "4.0.3.4", "3.12.3.3+fabric-1.20.1", "1.2.3-"} {
		if !Parse(s).IsSemantic() {
			t.Errorf("Parse(%q) is not semantic", s)
		}
	}
	for _, s := range []string{"0.0.-1", "0.2147483648.0", "0.-1.0", "-1.0.0", "", "0.0.a", "0.a.0", "a.0.0", "2.x", "1..2", ".1", "1."} {
		if Parse(s).IsSemantic() {
			t.Errorf("Parse(%q) is semantic, want a string version", s)
		}
	}
}

// The cases are Fabric Loader's own VersionParsingTests.
func TestPredicate(t *testing.T) {
	cases := []struct {
		predicate, version string
		want               bool
	}{
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.2", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.2.1", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.3", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.4+build.125", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.7", true},
		{">=0.3.1-beta.2 <0.4.0", "0.4.0-alpha.1", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.4-beta.7", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.11", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.0", false},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.1", false},
		{">=0.3.1-beta.2 <0.4.0", "0.4.0", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.2", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.2.1", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.3", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.4+build.125", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.7", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.4-beta.7", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.11", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.0", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.1", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.4.0-alpha.1", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.4.0", false},
		{">=1.4-", "1.4-beta.2", true},
		{">=1.4-", "1.4+build.125", true},
		{">=1.4-", "1.4", true},
		{">=1.4-", "1.4.2", true},
		{">=1.4-", "1.3", false},
		{">=1.4-", "1.3.5", false},
		{">=1.4-", "1.3-alpha.1", false},
		{"<1.4", "1.3", true},
		{"<1.4", "1.3.5", true},
		{"<1.4", "1.3-alpha.1", true},
		{"<1.4", "1.4-beta.2", true},
		{"<1.4", "1.4+build.125", false},
		{"<1.4", "1.4", false},
		{"<1.4-", "1.3", true},
		{"<1.4-", "1.3.5", true},
		{"<1.4-", "1.3-alpha.1", true},
		{"<1.4-", "1.4-beta.2", false},
		{"<1.4-", "1.4+build.125", false},
		{"<1.4-", "1.4", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.9", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.11", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.e", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.d.10", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.9.d.5", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.final", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.-final-", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.7", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.d", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.a", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-alpha.9", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.8", false},
		{"1.3.x", "1.3.0", true},
		{"1.3.x", "1.3.0-alpha.1", true},
		{"1.3.x", "1.3.99", true},
		{"1.3.x", "1.4.0", false},
		{"1.3.x", "1.2.9", false},
		{"1.3.x", "1.2.9-rc.6", false},
		{"1.3.x", "1.4.0-alpha.1", false},
		{"1.3.x", "2.0.0", false},
		{"2.x", "2.0.0", true},
		{"2.x", "2.0.0-alpha.1", true},
		{"2.x", "2.9.0-beta.2", true},
		{"2.x", "2.2.4", true},
		{"2.x", "1.99.99", false},
		{"2.x", "3.0.0", false},
		{"2.x", "3.0.0-alpha.1", false},
		{"~1.2.3", "1.2.3", true},
		{"~1.2.3", "1.2.4", true},
		{"~1.2.3", "1.2.4-alpha.1", true},
		{"~1.2.3", "1.2.2", false},
		{"~1.2.3", "1.2.3-rc.7", false},
		{"~1.2.3", "1.3.0", false},
		{"~1.2.3", "2.2.0", false},
		{"~1.2", "1.2.0", true},
		{"~1.2", "1.2.1-alpha.3", true},
		{"~1.2", "1.2.6", true},
		{"~1.2", "1.1.9", false},
		{"~1.2", "1.3.0", false},
		{"~1.2", "1.2.0-rc.2", false},
		{"~1.2", "1.3.0-alpha.3", false},
		{"~1.2-", "1.2.0", true},
		{"~1.2-", "1.2.1-alpha.3", true},
		{"~1.2-", "1.2.6", true},
		{"~1.2-", "1.2.0-rc.2", true},
		{"~1.2-", "1.1.9", false},
		{"~1.2-", "1.3.0", false},
		{"~1.2-", "1.3.0-alpha.3", false},
		{"~1", "1.0.0", true},
		{"~1", "1.0.4", true},
		{"~1", "0.9.9", false},
		{"~1", "1.1.5", false},
		{"~1", "3.0.5", false},
		{"~1.2.3-beta.2", "1.2.3-beta.2", true},
		{"~1.2.3-beta.2", "1.2.3-beta.2.1", true},
		{"~1.2.3-beta.2", "1.2.3-beta.3", true},
		{"~1.2.3-beta.2", "1.2.3-beta.11", true},
		{"~1.2.3-beta.2", "1.2.3-rc.7", true},
		{"~1.2.3-beta.2", "1.2.3", true},
		{"~1.2.3-beta.2", "1.2.5", true},
		{"~1.2.3-beta.2", "1.2.4-alpha.4", true},
		{"~1.2.3-beta.2", "1.3.0", false},
		{"~1.2.3-beta.2", "1.2.2", false},
		{"~1.2.3-beta.2", "1.2.3-beta.1", false},
		{"~1.2.3-beta.2", "1.2.3-beta.1.9", false},
		{"~1.2.3-beta.2", "1.2.3-alpha.4", false},
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.2.4", true},
		{"^1.2.3", "1.3.0", true},
		{"^1.2.3", "1.2.4-beta.2", true},
		{"^1.2.3", "1.2.2", false},
		{"^1.2.3", "1.2.3-beta.2", false},
		{"^1.2.3", "2.0.0", false},
		{"^0.2.3", "0.2.3", true},
		{"^0.2.3", "0.2.4", true},
		{"^0.2.3", "0.2.8-beta.2", true},
		{"^0.2.3", "0.3.0", true},
		{"^0.2.3", "0.2.0", false},
		{"^0.2.3", "0.2.3-rc.8", false},
		{"^0.2.3", "1.2.0", false},
		{"^1.2.3-beta.2", "1.2.3-beta.2", true},
		{"^1.2.3-beta.2", "1.2.3-beta.3", true},
		{"^1.2.3-beta.2", "1.2.3-rc.7", true},
		{"^1.2.3-beta.2", "1.2.3", true},
		{"^1.2.3-beta.2", "1.2.5", true},
		{"^1.2.3-beta.2", "1.3.0", true},
		{"^1.2.3-beta.2", "1.2.4-alpha.4", true},
		{"^1.2.3-beta.2", "1.2.2", false},
		{"^1.2.3-beta.2", "2.0.0", false},
		{"^1.2.3-beta.2", "1.2.3-alpha.4", false},
		{"^1", "1.0.0", true},
		{"^1", "1.2.4", true},
		{"^1", "1.99.99", true},
		{"^1", "1.2.4-beta.2", true},
		{"^1", "0.9.6", false},
		{"^1", "1.0.0-rc.5", false},
		{"^1", "2.0.0", false},
		{"^1", "2.0.0-beta.2", false},
		{"^1-", "1.0.0", true},
		{"^1-", "1.0.0-rc.5", true},
		{"^1-", "1.2.4", true},
		{"^1-", "1.99.99", true},
		{"^1-", "1.2.4-beta.2", true},
		{"^1-", "0.9.0", false},
		{"^1-", "0.9.0-rc.5", false},
		{"^1-", "2.0.0", false},
		{"^1-", "2.0.0-beta.2", false},
		{"1.2.3.x", "1.2.2", false},
		{"1.2.3.x", "1.2.3", true},
		{"1.2.3.x", "1.2.3-", true},
		{"1.2.3.x", "1.2.3.4", true},
		{"1.2.3.x", "1.2.3.4.5", true},
		{"1.2.3.x", "1.2.4", false},
		{"1.2.3.x", "1.2.4-", false},
		{"1.2.3.x", "1.2", false},
		{"1.2.3.x", "1.3", false},
		{"1.2.3.x", "1", false},
		{"1.2.3.x", "2", false},
	}
	for _, c := range cases {
		p, err := ParsePredicate(c.predicate)
		if err != nil {
			t.Fatalf("ParsePredicate(%q): %v", c.predicate, err)
		}
		if got := p.Test(Parse(c.version)); got != c.want {
			t.Errorf("%q.Test(%q) = %v, want %v", c.predicate, c.version, got, c.want)
		}
	}
}

func TestPredicateBeyondFabricsSuite(t *testing.T) {
	cases := []struct {
		predicate, version string
		want               bool
	}{
		{">=4.0.3.2", "4.0.3.4", true},
		{">=4.0.3.5", "4.0.3.4", false},
		{">=3.12.2", "3.12.3.3+fabric-1.20.1", true},
		{">=1.2.10.1", "1.2.11.3", true},
		{">=1.0", "1.0.0.0", true},
		{"=1.0", "1.0.0+build", true},
		{">=1.0", "custom-build", false},
		{"custom-build", "custom-build", true},
		{">=custom-build", "custom-build", true},
		{"<=custom-build", "other", false},
		{"1.2.x", "1.2.9.9", true},
		{"*", "anything at all", true},
		{"", "1.0", true},
		{"007", "7", true},
		{"1.x.2", "1.x.2", true},
		{"x", "1.0", false},
	}
	for _, c := range cases {
		p, err := ParsePredicate(c.predicate)
		if err != nil {
			t.Fatalf("ParsePredicate(%q): %v", c.predicate, err)
		}
		if got := p.Test(Parse(c.version)); got != c.want {
			t.Errorf("%q.Test(%q) = %v, want %v", c.predicate, c.version, got, c.want)
		}
	}
}

func TestPredicateInvalid(t *testing.T) {
	for _, s := range []string{">=1.x", "<custom", ">custom", ">="} {
		if _, err := ParsePredicate(s); err == nil {
			t.Errorf("ParsePredicate(%q) parsed, want an error", s)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"10.0.0.1", "9.0.0.1", 1},
		{"1.0", "1.0.0", 0},
		{"1.0-beta.2", "1.0-beta.10", -1},
		{"1.0-beta", "1.0-beta.1", -1},
		{"1.0-1", "1.0-a", -1},
		{"1.0-", "1.0-alpha", -1},
		{"1.0+a", "1.0+b", 0},
		{"abc", "abd", -1},
	} {
		if got := Compare(Parse(c.a), Parse(c.b)); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
