package project

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{"My Pack": "my-pack", "tmp.KEnt9tWC1o": "tmp.kent9twc1o", "---": "shulker-project", "west_coast SMP!": "west_coast-smp"}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
