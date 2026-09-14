package out

import (
	"testing"
	"time"
)

func TestSlowAside(t *testing.T) {
	for _, c := range []struct {
		elapsed time.Duration
		host    string
		want    string
	}{
		{2900 * time.Millisecond, "api.curseforge.com", ""},
		{8200 * time.Millisecond, "api.curseforge.com", " (8s, waiting on api.curseforge.com)"},
		{4 * time.Second, "", " (4s)"},
	} {
		if got := slowAside(c.elapsed, c.host); got != c.want {
			t.Errorf("slowAside(%v, %q) = %q, want %q", c.elapsed, c.host, got, c.want)
		}
	}
}

func TestWaitingNamesTheLatestHost(t *testing.T) {
	p := &Printer{}
	doneA := p.Waiting("a.example")
	doneB := p.Waiting("b.example")
	if got := p.waits.latest(); got != "b.example" {
		t.Fatalf("latest = %q", got)
	}
	doneB()
	doneB()
	if got := p.waits.latest(); got != "a.example" {
		t.Fatalf("after b finished, latest = %q", got)
	}
	doneA()
	if got := p.waits.latest(); got != "" {
		t.Fatalf("nothing in flight, latest = %q", got)
	}
}
