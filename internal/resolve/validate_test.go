package resolve

import "testing"

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, declared string
		want              bool
	}{
		{"0.17.3", ">=0.17", true},
		{"0.16.9", ">=0.17", false},
		{"0.18.0-beta.1", ">=0.17", true},
		{"26.2", "26.2", true},
		{"26.2", "*", true},
		{"26.3-pre-1", "*", true},
		{"26.2", "26.x", true},
		{"26.2", "~26.2-", true},
		{"26.3-pre-1", ">=26.2-", true},
		{"26.3-pre-1", ">=26.3", false},
		{"24w33a", ">=1.21.2-", true},
		{"24w33a", ">=1.21.2", false},
		{"24w33a", ">1.21.1", true},
		{"27.1", "26.x", false},
		{"0.5.2", "0.x", true},
		{"1.0.0", "0.x", false},
		{"26.2.1", "26.2.x", true},
		{"26.3", "26.2.x", false},
		{"0.130.0+26.2", ">=0.130.0", true},
		{"0.129.0+26.2", ">=0.130.0", false},
		{"25.0", ">=21", true},
		{"17.0", ">=21", false},
		{"1.2.3", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"1.9.0", "^1.2.0", true},
		{"2.0.0", "^1.2.0", false},
		{"1.0.0", "<0.9 || >=1.0", true},
		{"0.9.5", "<0.9 || >=1.0", false},
		{"1.0.0", ">=0.5 <2.0", true},
	}
	for _, c := range cases {
		got, err := satisfies(c.version, c.declared)
		if err != nil {
			t.Errorf("satisfies(%q, %q): %v", c.version, c.declared, err)
			continue
		}
		if got != c.want {
			t.Errorf("satisfies(%q, %q) = %v, want %v", c.version, c.declared, got, c.want)
		}
	}
}

func TestSatisfiesUnparsable(t *testing.T) {
	for _, c := range [][2]string{{"1.2.3.4", "*"}, {"1.0.0", ">=1.x"}, {"1.0.0", "1.2.3.4"}, {"26w10a", ">=26.2"}} {
		if _, err := satisfies(c[0], c[1]); err == nil {
			t.Errorf("satisfies(%q, %q) should not parse", c[0], c[1])
		}
	}
}
