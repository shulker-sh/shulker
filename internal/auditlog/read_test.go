package auditlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSkipsLinesThatArentEntries(t *testing.T) {
	path := writeLog(t,
		`{"at":"2026-09-19T18:02:04.000Z","group":"launchers","cmd":"sync","level":"info","msg":"start"}`,
		`not json`,
		``,
		`{"at":"2026-09-19T18:02:11.000Z","group":"launchers","cmd":"hook wrap","level":"error","code":"launch-not-started","msg":"can't run Java"}`,
	)
	entries, err := Read(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Cmd != "sync" || entries[1].Code != "launch-not-started" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestReadStopsAtTheSizeGiven(t *testing.T) {
	first := `{"at":"2026-09-19T18:02:04.000Z","cmd":"sync","level":"info","msg":"start"}`
	path := writeLog(t, first, `{"at":"2026-09-19T18:02:05.000Z","cmd":"log","level":"info","msg":"start"}`)
	entries, err := Read(path, int64(len(first)+1))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Cmd != "sync" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestReadOfNoLogIsEmpty(t *testing.T) {
	entries, err := Read(filepath.Join(t.TempDir(), FileName), -1)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}
}

func TestNewNotesTheSizeBeforeTheRun(t *testing.T) {
	path := writeLog(t, `{"at":"2026-09-19T18:02:04.000Z","cmd":"sync","level":"info","msg":"start"}`)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	l := New(path, []string{"log"})
	l.Start("log", "shulker", "", nil)
	if l.Before != info.Size() {
		t.Fatalf("Before = %d, want %d", l.Before, info.Size())
	}
	if entries, _ := Read(path, l.Before); len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	if New(filepath.Join(t.TempDir(), FileName), nil).Before != 0 {
		t.Fatal("a log that isn't there yet has nothing before the run")
	}
}

func TestFilterNarrowsAndCombines(t *testing.T) {
	at := time.Date(2026, 9, 19, 18, 0, 0, 0, time.UTC)
	stamp := func(d time.Duration) string { return at.Add(d).Format(time.RFC3339Nano) }
	sync := Entry{At: stamp(0), Group: "launchers", Cmd: "sync", Instance: "friends", Level: LevelInfo, Msg: "start"}
	wrap := Entry{At: stamp(time.Minute), Group: "launchers", Cmd: "hook wrap", Instance: "/games/friends", Level: LevelError, Code: "launch-not-started"}
	add := Entry{At: stamp(-time.Hour), Group: "mods", Cmd: "add", Level: LevelWarn, Msg: "jei has no build"}
	all := []Entry{sync, wrap, add}
	cases := []struct {
		name   string
		filter Filter
		want   []Entry
	}{
		{"nothing", Filter{}, all},
		{"since", Filter{Since: at.Add(-time.Minute)}, []Entry{sync, wrap}},
		{"group", Filter{Group: "launchers"}, []Entry{sync, wrap}},
		{"cmd exact", Filter{Cmd: "hook wrap"}, []Entry{wrap}},
		{"cmd parent", Filter{Cmd: "hook"}, []Entry{wrap}},
		{"cmd is words, not a prefix", Filter{Cmd: "sy"}, nil},
		{"code", Filter{Code: "launch-not-started"}, []Entry{wrap}},
		{"level", Filter{Level: LevelWarn}, []Entry{add}},
		{"instance by any of its names", Filter{Instance: []string{"friends", "Friends SMP", "/games/friends"}}, []Entry{sync, wrap}},
		{"combined", Filter{Group: "launchers", Level: LevelInfo, Instance: []string{"friends"}}, []Entry{sync}},
		{"combined to nothing", Filter{Group: "mods", Code: "launch-not-started"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []Entry
			for _, e := range all {
				if c.filter.Match(e) {
					got = append(got, e)
				}
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
			for i := range got {
				if got[i].Cmd != c.want[i].Cmd {
					t.Fatalf("got %+v, want %+v", got, c.want)
				}
			}
		})
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"24h":                  now.Add(-24 * time.Hour),
		"90m":                  now.Add(-90 * time.Minute),
		"7d":                   now.AddDate(0, 0, -7),
		"2026-09-01":           time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local),
		"2026-09-01T10:00:00Z": time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := ParseSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "yesterday", "-3d", "0h", "d"} {
		if _, err := ParseSince(in, now); err == nil {
			t.Errorf("ParseSince(%q) took it", in)
		}
	}
}
