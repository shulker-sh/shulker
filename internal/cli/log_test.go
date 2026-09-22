package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/auditlog"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
)

type logFixture struct {
	path string
	game string
}

// seedLog writes a log of four entries, one of them two days old, and registers the instance the
// launch entries name, so -i has something to resolve.
func seedLog(t *testing.T) logFixture {
	t.Helper()
	path := isolatedLog(t)
	game := t.TempDir()
	registry := map[string]any{"$schema": config.RegistrySchemaURL, "instances": []config.Instance{{
		ID: "friends", Launcher: "prism", Name: "Friends SMP", Dir: game, Source: game,
	}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(path), "registry.json"), string(data))
	ago := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339Nano) }
	entries := []auditlog.Entry{
		{At: ago(48 * time.Hour), Group: "mods", Cmd: "add", Level: auditlog.LevelWarn, Msg: "jei has no build for 26.2"},
		{At: ago(2 * time.Hour), Group: "launchers", Cmd: "sync", Instance: "friends", Level: auditlog.LevelInfo, Msg: "start"},
		{At: ago(90 * time.Minute), Group: "launchers", Cmd: "hook wrap", Instance: game, Level: auditlog.LevelError, Code: "launch-not-started", Msg: "can't run Java at /x/java: no such file"},
		{At: ago(time.Hour), Group: "shulker", Cmd: "cache prune", Level: auditlog.LevelInfo, Msg: "start"},
	}
	writeFile(t, path, logLines(t, entries...))
	return logFixture{path: path, game: game}
}

// logLines is entries as log.jsonl stores them.
func logLines(t *testing.T, entries ...auditlog.Entry) string {
	t.Helper()
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(line, '\n'))
	}
	return b.String()
}

func logReportOf(t *testing.T, stdout string) logReport {
	t.Helper()
	var env struct {
		OK   bool      `json:"ok"`
		Data logReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || !env.OK {
		t.Fatalf("envelope %q: %v", stdout, err)
	}
	return env.Data
}

func logCmds(r logReport) []string {
	var cmds []string
	for _, e := range r.Entries {
		cmds = append(cmds, e.Cmd)
	}
	return cmds
}

func TestLogShowsTheLastDayWithItsPreamble(t *testing.T) {
	seedLog(t)
	code, stdout, stderr := run(t, "log", "--no-color")
	if code != out.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"shulker dev (" + runtime.GOOS + "/" + runtime.GOARCH + ", last 24h of 30 days kept, redacted)",
		"every entry (3 of 4 entries)",
		"sync",
		"✘ ",
		"launch-not-started",
		"can't run Java at /x/java: no such file",
		"cache prune",
		"Widen with:",
		"$ shulker log --since 30d",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "jei has no build") {
		t.Errorf("a two-day-old entry is outside the default window:\n%s", stdout)
	}
	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		if strings.Contains(line, "launch-not-started") {
			if !strings.HasPrefix(line, "✘ ") || i+1 >= len(lines) || !strings.Contains(lines[i+1], "can't run Java") {
				t.Errorf("an error is marked and has its message on the next line:\n%s", stdout)
			}
		}
	}
}

func TestLogIndentsEveryLineOfAMessage(t *testing.T) {
	path := isolatedLog(t)
	at := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	writeFile(t, path, logLines(t,
		auditlog.Entry{At: at, Group: "launchers", Cmd: "sync", Level: auditlog.LevelError, Code: "source-offline", Msg: "couldn't reach the pack\nit has never synced here"},
		auditlog.Entry{At: at, Group: "launchers", Cmd: "sync", Level: auditlog.LevelWarn, Msg: "kept the old copy\nit is a day old"},
	))
	code, stdout, stderr := run(t, "log", "--no-color")
	if code != out.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	column := func(line, text string) int {
		i := strings.Index(line, text)
		if i < 0 {
			t.Fatalf("%q not on its line in:\n%s", text, stdout)
		}
		return out.Width(line[:i])
	}
	printed := strings.Split(stdout, "\n")
	find := func(text string) string {
		for _, line := range printed {
			if strings.Contains(line, text) {
				return line
			}
		}
		t.Fatalf("missing %q in:\n%s", text, stdout)
		return ""
	}
	for first, next := range map[string]string{"couldn't reach the pack": "it has never synced here", "kept the old copy": "it is a day old"} {
		if a, b := column(find(first), first), column(find(next), next); a != b {
			t.Errorf("%q starts at column %d and the line after it at %d:\n%s", first, a, b, stdout)
		}
	}
}

func TestLogJSONIsTheEnvelope(t *testing.T) {
	seedLog(t)
	code, stdout, _ := run(t, "log", "--json", "--since", "7d")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	r := logReportOf(t, stdout)
	if r.Version != "dev" || r.Platform != runtime.GOOS+"/"+runtime.GOARCH || r.Since != "7d" || r.KeepDays != 30 || r.Read != 4 || r.Matched != 4 || len(r.Entries) != 4 {
		t.Fatalf("report = %+v", r)
	}
}

func TestLogLeavesOutItsOwnRun(t *testing.T) {
	path := isolatedLog(t)
	run(t, "instances", "repair")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := run(t, "log", "--json")
	r := logReportOf(t, stdout)
	if r.Read == 0 || slices.ContainsFunc(r.Entries, func(e auditlog.Entry) bool { return e.Cmd != "instances repair" }) {
		t.Fatalf("report = %+v", r)
	}
	run(t, "log", "--since", "nope")
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatalf("the log run logged itself:\n%s", after)
	}
}

func TestLogFiltersNarrowAndCombine(t *testing.T) {
	const game = "<game>"
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--since", "30d"}, "add,sync,hook wrap,cache prune"},
		{[]string{"--group", "launchers"}, "sync,hook wrap"},
		{[]string{"--cmd", "hook"}, "hook wrap"},
		{[]string{"--cmd", "cache prune"}, "cache prune"},
		{[]string{"--code", "launch-not-started"}, "hook wrap"},
		{[]string{"--level", "info"}, "sync,cache prune"},
		{[]string{"--since", "30d", "--level", "warn"}, "add"},
		{[]string{"-i", "friends"}, "sync,hook wrap"},
		{[]string{"-i", "Friends SMP"}, "sync,hook wrap"},
		{[]string{"-i", game}, "sync,hook wrap"},
		{[]string{"-i", "friends", "--level", "error"}, "hook wrap"},
		{[]string{"--group", "launchers", "--cmd", "sync"}, "sync"},
		{[]string{"--group", "mods"}, ""},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			f := seedLog(t)
			args := slices.Clone(c.args)
			if i := slices.Index(args, game); i >= 0 {
				args[i] = f.game
			}
			code, stdout, stderr := run(t, append([]string{"log", "--json"}, args...)...)
			if code != out.ExitOK {
				t.Fatalf("exit %d %s %s", code, stdout, stderr)
			}
			r := logReportOf(t, stdout)
			if got := strings.Join(logCmds(r), ","); got != c.want || r.Matched != len(r.Entries) {
				t.Errorf("got %q (matched %d), want %q", got, r.Matched, c.want)
			}
		})
	}
}

func TestLogFilterLineNamesTheFilters(t *testing.T) {
	seedLog(t)
	_, stdout, _ := run(t, "log", "--no-color", "--group", "launchers", "--level", "error")
	if !strings.Contains(stdout, "group launchers, level error (1 of 4 entries)") {
		t.Fatalf("filter line:\n%s", stdout)
	}
}

func TestLogEmptyMatchStillPrintsThePreamble(t *testing.T) {
	seedLog(t)
	code, stdout, _ := run(t, "log", "--no-color", "--code", "nothing-like-this")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"last 24h of 30 days kept", "code nothing-like-this (0 of 4 entries)", "$ shulker log --since 30d"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestLogWithNoLogIsAnEmptyReport(t *testing.T) {
	isolatedLog(t)
	code, stdout, stderr := run(t, "log", "--no-color")
	if code != out.ExitOK || !strings.Contains(stdout, "every entry (0 of 0 entries)") || stderr != "" {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
}

func TestLogThatCantBeReadWarns(t *testing.T) {
	path := isolatedLog(t)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := run(t, "log", "--json")
	r := logReportOf(t, stdout)
	if code != out.ExitOK || r.Read != 0 || !strings.Contains(stdout, "can't read shulker's log") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestLogWidestWindowHasNoNudge(t *testing.T) {
	seedLog(t)
	_, stdout, _ := run(t, "log", "--no-color", "--since", "30d")
	if strings.Contains(stdout, "Widen with") || !strings.Contains(stdout, "last 30d of 30 days kept") {
		t.Fatalf("output:\n%s", stdout)
	}
}

func TestLogRefusesABadWindowOrLevel(t *testing.T) {
	isolatedLog(t)
	for _, args := range [][]string{{"--since", "yesterday"}, {"--level", "debug"}} {
		code, stdout, _ := run(t, append([]string{"log", "--json"}, args...)...)
		if code == out.ExitOK || failureCode(t, stdout).Code != "usage" {
			t.Errorf("%v: exit %d %s", args, code, stdout)
		}
	}
}

func TestLogAfterATrimReadsWhatWasKept(t *testing.T) {
	f := seedLog(t)
	writeFile(t, filepath.Join(filepath.Dir(f.path), "config.json"), `{"log":{"keepDays":1}}`)
	seeded, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	// Longer than this run's own start, so reading the log at its size before the trim would take
	// that start in.
	old := `{"at":"2026-01-01T00:00:00Z","group":"mods","cmd":"add","level":"warn","msg":"` + strings.Repeat("x", 2000) + `"}` + "\n"
	writeFile(t, f.path, old+string(seeded))
	code, stdout, _ := run(t, "log", "--json", "--since", "7d")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	r := logReportOf(t, stdout)
	if r.KeepDays != 1 || r.Read != 3 || !slices.Equal(logCmds(r), []string{"sync", "hook wrap", "cache prune"}) {
		t.Fatalf("report = %+v", r)
	}
}

const logKey = "$2a$10$bL4bIL5pUWqfcO7KQtnMReakwtfHbNKh6v1uTpKlzhwoueEJQnPnm"

// seedSecretLog writes a log whose entries hold a credentialed source URL, the configured CurseForge
// key and paths under the home directory, which the test points at a temp dir.
func seedSecretLog(t *testing.T) (home string) {
	t.Helper()
	path := isolatedLog(t)
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if code, _, stderr := run(t, "config", "set", "curseforge.key", logKey); code != out.ExitOK {
		t.Fatalf("config set: exit %d: %s", code, stderr)
	}
	game := filepath.Join(home, "Games", "friends")
	at := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seeded := logLines(t,
		auditlog.Entry{At: at, Group: "mods", Cmd: "search", Level: auditlog.LevelWarn, Msg: "curseforge: GET https://api.curseforge.com/v1/mods/search?key=" + logKey + " failed"},
		auditlog.Entry{At: at, Group: "launchers", Cmd: "sync", Instance: game, Level: auditlog.LevelError, Code: "source-fetch-failed", Msg: "can't clone https://ghp_s3cr3t@github.com/org/pack.git into " + game},
	)
	if _, err := f.WriteString(seeded); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestLogRedactsByDefault(t *testing.T) {
	home := seedSecretLog(t)
	for _, args := range [][]string{{"log", "--no-color"}, {"log", "--json"}} {
		code, stdout, stderr := run(t, args...)
		if code != out.ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, stderr)
		}
		for _, secret := range []string{logKey, "ghp_s3cr3t", home} {
			if strings.Contains(stdout, secret) || strings.Contains(stdout, strings.ReplaceAll(secret, `\`, `\\`)) {
				t.Errorf("%v shows %q:\n%s", args, secret, stdout)
			}
		}
		for _, want := range []string{"https://github.com/org/pack.git", filepath.Join("~", "Games", "friends"), "[key]"} {
			if !strings.Contains(stdout, strings.ReplaceAll(want, `\`, `\\`)) && !strings.Contains(stdout, want) {
				t.Errorf("%v is missing %q:\n%s", args, want, stdout)
			}
		}
	}
	_, stdout, _ := run(t, "log", "--no-color")
	if !strings.Contains(stdout, "days kept, redacted)") {
		t.Errorf("the preamble says the output is redacted:\n%s", stdout)
	}
	_, stdout, _ = run(t, "log", "--json")
	if !logReportOf(t, stdout).Redacted {
		t.Errorf("--json says it is redacted:\n%s", stdout)
	}
}

func TestLogUnredactedPrintsEntriesAsStored(t *testing.T) {
	home := seedSecretLog(t)
	code, stdout, stderr := run(t, "log", "--no-color", "--unredacted")
	if code != out.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"days kept, unredacted)", logKey, "https://ghp_s3cr3t@github.com/org/pack.git", filepath.Join(home, "Games", "friends")} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	_, stdout, _ = run(t, "log", "--json", "--unredacted")
	r := logReportOf(t, stdout)
	if r.Redacted || len(r.Entries) == 0 || r.Entries[len(r.Entries)-1].Instance != filepath.Join(home, "Games", "friends") {
		t.Fatalf("report = %+v", r)
	}
}
