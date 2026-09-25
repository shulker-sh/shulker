package out

import (
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPicks(t *testing.T) {
	six := []string{"lithium", "iris", "modmenu", "spark", "chunky", "krypton"}
	for _, tc := range []struct {
		name  string
		e     Error
		label string
		want  []string
	}{
		{"a near miss lists the closest", Error{Given: "sodim", Candidates: []string{"iris", "sodium", "sodium-extra", "lithium"}}, "did you mean", []string{"sodium"}},
		{"a choice lists every option", Error{Candidates: []string{"client", "dev"}}, "pick one", []string{"client", "dev"}},
		{"nothing close in a short list lists them all", Error{Given: "zzz", Candidates: []string{"client", "server"}}, "pick one", []string{"client", "server"}},
		{"nothing close in a long list lists nothing", Error{Given: "zzzzzz", Candidates: six}, "", nil},
		{"a pick can read one way and pass another", Error{Given: "server.vew-distance", Candidates: []string{"view-distance", "motd"}, Pass: []string{"server.view-distance", "server.motd"}}, "did you mean", []string{"view-distance"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			label, picks := tc.e.picks()
			var shown []string
			for _, p := range picks {
				shown = append(shown, p.Show)
			}
			if label != tc.label || !slices.Equal(shown, tc.want) {
				t.Fatalf("got %q %v, want %q %v", label, shown, tc.label, tc.want)
			}
		})
	}
}

func TestExampleCommand(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		e     Error
		value string
		want  string
	}{
		{"replaces the typed argument", []string{"remove", "sodim"}, Error{Given: "sodim"}, "sodium", "shulker remove sodium"},
		{"replaces a flag value written with =", []string{"sync", "--side=dve"}, Error{Given: "dve"}, "dev", "shulker sync --side=dev"},
		{"drops the typo when the pick is already typed", []string{"mod", "add"}, Error{Given: "mod"}, "add", "shulker add"},
		{"sets a flag that was given", []string{"export", "--side", "client", "-o", "x.mrpack"}, Error{Flag: "--side"}, "server", "shulker export --side server -o x.mrpack"},
		{"adds a flag that was missing", []string{"sync"}, Error{Flag: "--side"}, "client", "shulker sync --side client"},
		{"the typed argument wins over the flag", []string{"create", "--side", "clint"}, Error{Given: "clint", Flag: "--side"}, "client", "shulker create --side client"},
		{"a value typed twice has no example", []string{"remove", "sodim", "sodim"}, Error{Given: "sodim"}, "sodium", ""},
		{"quotes an argument with spaces", []string{"pull"}, Error{Flag: "--into"}, "/Users/me/Application Support/x", "shulker pull --into '/Users/me/Application Support/x'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(tc.want, "'") {
				t.Skip("Windows quotes with double quotes")
			}
			got, ok := exampleCommand(tc.args, &tc.e, tc.value)
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("got %q %v, want %q", got, ok, tc.want)
			}
		})
	}
}

func TestFailShowsPicksAndAnExampleCommand(t *testing.T) {
	var stderr strings.Builder
	p := &Printer{Stdout: io.Discard, Stderr: &stderr, Args: []string{"sync"}}
	e := Errorf("ambiguous-side", "shulker.json declares both sides; choose one")
	e.Candidates, e.Flag = []string{"client", "server"}, "--side"
	p.Fail(e)
	want := "  ✘ error: shulker.json declares both sides; choose one (ambiguous-side)\n" +
		"    ╰─ pick one:\n" +
		"         ├─ ‣ client\n" +
		"         ╰─ ‣ server\n" +
		"\n" +
		"  For example:\n" +
		"    $ shulker sync --side client\n"
	if stderr.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", stderr.String(), want)
	}
}
