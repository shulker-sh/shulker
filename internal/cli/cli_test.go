package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/selfupdate"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestVersionHuman(t *testing.T) {
	code, stdout, _ := run(t, "version")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if lines := strings.Split(stdout, "\n"); len(lines) != 2 || !strings.HasPrefix(lines[0], "  shulker dev") || lines[1] != "" {
		t.Fatalf("one line, padded: %q", stdout)
	}
	code, stdout, _ = run(t, "version", "--verbose")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, label := range []string{"Go        go", "Binary    ", "Config    ", "Cache     "} {
		if !strings.Contains(stdout, "\n  "+label) {
			t.Fatalf("missing %q row in %q", strings.TrimSpace(label), stdout)
		}
	}
}

func TestVersionNamesTheRouteOnlyWhenVerbose(t *testing.T) {
	h := newHarness(t)
	h.build = &selfupdate.Build{Version: "0.0.1", Built: "2026-09-20T14:02:00Z", Route: selfupdate.Release}
	if stdout := h.mustRun(t, "version"); stdout != "  shulker 0.0.1 (built 2026-09-20 14:02 UTC)\n" {
		t.Fatalf("release: %q", stdout)
	}
	stdout := h.mustRun(t, "version", "--verbose")
	if !strings.HasPrefix(stdout, "  shulker 0.0.1\n\n  Built     2026-09-20 14:02 UTC\n") || !strings.Contains(stdout, "\n  Install   release\n") {
		t.Fatalf("release --verbose: %q", stdout)
	}

	h.build = &selfupdate.Build{Version: "0.0.1", Route: selfupdate.GoInstall}
	if stdout := h.mustRun(t, "version"); stdout != "  shulker 0.0.1\n" {
		t.Fatalf("go install: %q", stdout)
	}
	if stdout := h.mustRun(t, "version", "--verbose"); !strings.Contains(stdout, "\n  Install   go install\n") || strings.Contains(stdout, "Built") {
		t.Fatalf("go install --verbose: %q", stdout)
	}

	h.build = &selfupdate.Build{Version: selfupdate.Dev, Commit: "d1556f95d232", Modified: true, Built: "2026-09-18T22:25:43Z", Route: selfupdate.Source}
	if stdout := h.mustRun(t, "version"); stdout != "  shulker dev  d1556f9-dirty (built 2026-09-18 22:25 UTC)\n" {
		t.Fatalf("source: %q", stdout)
	}
	if stdout := h.mustRun(t, "version", "--verbose"); !strings.Contains(stdout, "\n  Install   source\n") || strings.Contains(stdout, "(built") {
		t.Fatalf("source --verbose: %q", stdout)
	}

	h.build = &selfupdate.Build{Version: selfupdate.Dev}
	if stdout := h.mustRun(t, "version", "--verbose"); strings.Contains(stdout, "Install") {
		t.Fatalf("an unknown route says nothing: %q", stdout)
	}
	if data := h.versionData(t); data["install"] != nil {
		t.Fatalf("an unknown route is left out of the JSON: %+v", data)
	}
	h.build = &selfupdate.Build{Version: "0.0.1", Route: selfupdate.GoInstall}
	if data := h.versionData(t); data["install"] != "go install" || data["version"] != "0.0.1" {
		t.Fatalf("--json always carries the route: %+v", data)
	}
}

func (h *harness) versionData(t *testing.T) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(h.runSetting(t, 0, "version").Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestVersionJSON(t *testing.T) {
	code, stdout, _ := run(t, "version", "--json")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.Command != "version" || env.Error != nil || env.Warnings == nil {
		t.Fatalf("unexpected envelope %+v", env)
	}
	data := env.Data.(map[string]any)
	if data["version"] != "dev" {
		t.Fatalf("unexpected data %+v", data)
	}
}

func TestVersionShortCommit(t *testing.T) {
	for _, tc := range []struct{ commit, want string }{
		{"", ""},
		{"c2f0ca9", "c2f0ca9"},
		{"c2f0ca96de074c005b69009b87c24a4978944bd6", "c2f0ca9"},
	} {
		if got := (versionInfo{Commit: tc.commit}).shortCommit(); got != tc.want {
			t.Fatalf("shortCommit(%q) = %q, want %q", tc.commit, got, tc.want)
		}
	}
}

func TestHelpLinksDocs(t *testing.T) {
	for args, url := range map[string]string{
		"--help":              docsURL,
		"add --help":          docsURL + "/cli#shulker-add",
		"feature list --help": docsURL + "/cli#shulker-feature-list",
	} {
		code, stdout, _ := run(t, strings.Fields(args)...)
		if code != out.ExitOK || !strings.HasSuffix(stdout, "  Docs "+url+"\n  For agents "+agentsURL+"\n") {
			t.Fatalf("%v: code=%d stdout=%q", args, code, stdout)
		}
	}
}

func TestHelpShowsTheReferenceDescriptionAndExamples(t *testing.T) {
	_, stdout, _ := run(t, "add", "--help", "--no-color")
	if !strings.HasPrefix(stdout, "  Add mods to the manifest, resolve them") || !strings.Contains(stdout, "  Examples\n    $ shulker add sodium lithium\n") {
		t.Fatalf("add help:\n%s", stdout)
	}
	if strings.Contains(stdout, "(default: true)") || !strings.Contains(stdout, "More:\n    $ shulker docs add\n") {
		t.Fatalf("add help:\n%s", stdout)
	}
	if _, remove, _ := run(t, "remove", "--help", "--no-color"); strings.Contains(remove, "More:") {
		t.Fatalf("remove help ends at its examples:\n%s", remove)
	}
	_, lock, _ := run(t, "lock", "--help")
	description, _, _ := strings.Cut(lock, "\n  Usage\n")
	if !strings.Contains(description, "\n\n") {
		t.Fatalf("lock help lacks its second paragraph:\n%s", lock)
	}
	for _, line := range strings.Split(description, "\n") {
		if out.Width(line) > helpColumns || strings.Count(line, "`") > 0 {
			t.Fatalf("lock help line %q", line)
		}
	}
	if _, sync, _ := run(t, "sync", "--help"); !strings.Contains(sync, "  More:\n    $ shulker docs sync\n") {
		t.Fatalf("sync help:\n%s", sync)
	}
	if _, link, _ := run(t, "link", "--help"); strings.HasPrefix(link, "  shulker") || strings.HasPrefix(link, "\n") {
		t.Fatalf("link help:\n%s", link)
	}
}

func TestHelpJSON(t *testing.T) {
	for args, want := range map[string]string{
		"--help":              "",
		"help add":            "add",
		"add --help":          "add",
		"feature":             "feature",
		"feature list --help": "feature list",
	} {
		code, stdout, stderr := run(t, append(strings.Fields(args), "--json")...)
		var env struct {
			OK      bool     `json:"ok"`
			Command string   `json:"command"`
			Data    helpData `json:"data"`
		}
		if err := json.Unmarshal([]byte(stdout), &env); err != nil || code != out.ExitOK || stderr != "" {
			t.Fatalf("%s: code=%d err=%v stdout=%q stderr=%q", args, code, err, stdout, stderr)
		}
		if !env.OK || env.Data.Command != want || env.Data.Short == "" || env.Data.Usage == "" {
			t.Fatalf("%s: %+v", args, env)
		}
	}
	_, stdout, _ := run(t, "feature", "--json")
	var group struct{ Data helpData }
	_ = json.Unmarshal([]byte(stdout), &group)
	if len(group.Data.Commands) == 0 || group.Data.Commands[0].Name == "" {
		t.Fatalf("feature lists no commands: %s", stdout)
	}
	_, stdout, _ = run(t, "add", "--help", "--json")
	var add struct{ Data helpData }
	_ = json.Unmarshal([]byte(stdout), &add)
	if len(add.Data.Examples) == 0 || len(add.Data.Description) == 0 || !slices.ContainsFunc(add.Data.Flags, func(f helpDataFlag) bool { return f.Name == "side" }) {
		t.Fatalf("add help: %s", stdout)
	}
	if slices.ContainsFunc(add.Data.Flags, func(f helpDataFlag) bool { return f.Name == "json" }) || !slices.ContainsFunc(add.Data.GlobalFlags, func(f helpDataFlag) bool { return f.Name == "json" }) {
		t.Fatalf("add help puts --json in the wrong list: %s", stdout)
	}
	if !slices.ContainsFunc(add.Data.Flags, func(f helpDataFlag) bool { return f.Name == "as" && f.Type == "string" }) {
		t.Fatalf("add help gives --as no type: %s", stdout)
	}
	for _, f := range append(add.Data.Flags, add.Data.GlobalFlags...) {
		if f.Default == "0" || strings.ContainsAny(f.Default, "[]") {
			t.Fatalf("add help flag %s default %q", f.Name, f.Default)
		}
	}
	_, stdout, _ = run(t, "--help", "--json")
	var root struct{ Data helpData }
	_ = json.Unmarshal([]byte(stdout), &root)
	if !strings.Contains(strings.Join(root.Data.Description, "\n"), "error.code") {
		t.Fatalf("root help leaves out the agent note: %s", stdout)
	}
}

func TestCompletionScripts(t *testing.T) {
	for shell, marker := range map[string]string{"bash": "bash completion V2 for shulker", "zsh": "#compdef shulker", "fish": "fish completion for shulker", "powershell": "powershell completion for shulker"} {
		code, stdout, _ := run(t, "completion", shell)
		if code != out.ExitOK || !strings.Contains(stdout, marker) {
			t.Errorf("%s: exit %d, script lacks %q", shell, code, marker)
		}
		if code, stdout, _ := run(t, "completion", shell, "--no-descriptions"); code != out.ExitOK || stdout == "" {
			t.Errorf("%s --no-descriptions: exit %d", shell, code)
		}
	}
	if _, stdout, _ := run(t, "completion", "zsh", "--help"); !strings.HasPrefix(stdout, "  Print the zsh completion script, so Tab completes") || !strings.Contains(stdout, "$ source <(shulker completion zsh)\n") {
		t.Fatalf("completion zsh help:\n%s", stdout)
	}
	if _, stdout, _ := run(t, "__complete", "feature", "l"); !strings.Contains(stdout, "list") {
		t.Fatalf("__complete feature l = %q", stdout)
	}
}

func TestHelpCommandReportsUnknownTopics(t *testing.T) {
	for args, want := range map[string]string{
		"help nosuch":      `unknown command "nosuch"`,
		"help feature lst": `unknown command "lst" for "shulker feature"`,
	} {
		code, stdout, _ := run(t, append([]string{"--json"}, strings.Fields(args)...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || e.Message != want {
			t.Errorf("%s: exit %d, %+v", args, code, e)
		}
	}
	if _, _, stderr := run(t, "help", "feature", "lst"); !strings.Contains(stderr, "$ shulker help feature list\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	for help, flag := range map[string]string{"help add": "add --help", "help rm": "remove --help", "help feature list": "feature list --help"} {
		_, viaHelp, _ := run(t, strings.Fields(help)...)
		_, viaFlag, _ := run(t, strings.Fields(flag)...)
		if viaHelp == "" || viaHelp != viaFlag {
			t.Errorf("%q differs from %q:\n%s", help, flag, viaHelp)
		}
	}
	if code, stdout, _ := run(t, "help"); code != out.ExitOK || !strings.Contains(stdout, "  Usage\n") {
		t.Fatalf("help alone: exit %d\n%s", code, stdout)
	}
	if _, stdout, _ := run(t, "--help"); strings.Contains(stdout, "    help ") {
		t.Fatalf("help is listed:\n%s", stdout)
	}
}

func TestGroupCommandsReportUnknownSubcommands(t *testing.T) {
	for _, args := range [][]string{{"feature", "lst"}, {"self", "updte"}, {"config", "st", "key"}} {
		code, stdout, _ := run(t, append([]string{"--json"}, args...)...)
		e := failureCode(t, stdout)
		want := `unknown command "` + args[1] + `" for "shulker ` + args[0] + `"`
		if code != out.ExitUsage || e.Code != "usage" || e.Message != want || len(e.Candidates) == 0 {
			t.Errorf("%v: exit %d, %+v", args, code, e)
		}
	}
	_, _, stderr := run(t, "feature", "lst")
	for _, want := range []string{"did you mean:", "\u2023 list", "$ shulker feature list\n"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if code, stdout, _ := run(t, "feature"); code != out.ExitOK || !strings.Contains(stdout, "  Commands\n") {
		t.Fatalf("feature alone: exit %d\n%s", code, stdout)
	}
}

func TestUsageErrorsShowUsageAndPointAtHelp(t *testing.T) {
	_, _, stderr := run(t, "add")
	if !strings.HasSuffix(stderr, "\n    ├─ usage: shulker add <mod|source>... [flags]\n    ╰─ help: shulker add --help lists every flag\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "Flags") || strings.Contains(stderr, "Examples") || strings.HasSuffix(stderr, "\n\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if _, _, stderr := run(t, "player"); !strings.Contains(stderr, "usage: shulker player [name|uuid]") || !strings.Contains(stderr, "help: shulker player --help") {
		t.Fatalf("player stderr:\n%s", stderr)
	}
	if _, stdout, stderr := run(t, "add", "--json"); stderr != "" || strings.Contains(stdout, "Usage") {
		t.Fatalf("add --json stdout %q stderr %q", stdout, stderr)
	}
}

func TestEveryRunnableCommandDeclaresWhatItsRunsDo(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() && !slices.Contains([]string{logReads, logActs, logDecides, logNever}, c.Annotations[logMode]) {
			t.Errorf("%q declares neither reads nor acts, so the log can't tell whether to keep a clean run", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newApp(io.Discard, io.Discard).root())
}

func TestEveryHelpIsPlainText(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		args := append(strings.Fields(strings.TrimPrefix(c.CommandPath(), "shulker")), "--help", "--no-color")
		_, stdout, _ := run(t, args...)
		if strings.Contains(stdout, "**") {
			t.Errorf("%s help prints markdown emphasis:\n%s", c.CommandPath(), stdout)
		}
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasSuffix(line, "-") {
				t.Errorf("%s help breaks a line at a hyphen: %q", c.CommandPath(), line)
			}
		}
		c.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
			if f.Value.Type() == "bool" && strings.Contains(stdout, "--"+f.Name+" <") {
				t.Errorf("%s help gives the boolean --%s a value name", c.CommandPath(), f.Name)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newApp(io.Discard, io.Discard).root())
}

func TestRootHelpTellsAgentsToUseJSON(t *testing.T) {
	_, stdout, _ := run(t, "--help")
	for _, line := range strings.Split(agentHelp, "\n") {
		if !strings.Contains(stdout, "  "+line+"\n") {
			t.Fatalf("root help lacks %q:\n%s", line, stdout)
		}
	}
	if strings.Contains(stdout, "Other commands") {
		t.Fatalf("a root command is missing from helpGroups:\n%s", stdout)
	}
	root := newApp(io.Discard, io.Discard).root()
	for _, c := range root.Commands() {
		if helpGroupOf(c.Name()) == "" {
			t.Errorf("root command %q has no group in helpGroups, so its runs are logged under none", c.Name())
		}
		if c.Hidden && strings.Contains(stdout, "\n    "+c.Name()+" ") {
			t.Errorf("hidden command %q shows in root help", c.Name())
		}
	}
	if _, stdout, _ := run(t, "add", "--help"); strings.Contains(stdout, "Scripts and agents") {
		t.Fatalf("add help = %q", stdout)
	}
}

func TestAddRejectsBadFlagValues(t *testing.T) {
	for _, flag := range [][]string{{"--side", "top"}, {"--channel", "nightly"}, {"--provider", "github"}} {
		code, stdout, _ := run(t, append([]string{"--json", "add", "sodium"}, flag...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Message, flag[0]) {
			t.Errorf("%v: exit %d %s", flag, code, stdout)
		}
	}
}

func TestHumanErrorNamesCode(t *testing.T) {
	_, _, stderr := run(t, "add", "sodium", "--side", "top")
	if !strings.HasPrefix(stderr, "  ✘ --side takes one of client, server, both, not \"top\" (usage)\n    ├─ usage: shulker add") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestUnknownCommandJSON(t *testing.T) {
	code, stdout, stderr := run(t, "bogus", "--json")
	if code != out.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty in json mode, got %q", stderr)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("unexpected envelope %+v", env)
	}
}

func TestUsageErrorJSONNamesTheCommand(t *testing.T) {
	for args, want := range map[string]string{
		"list --bogus":  "list",
		"feature on":    "feature on",
		"bogus":         "",
		"feature bogus": "feature",
	} {
		_, stdout, _ := run(t, append(strings.Fields(args), "--json")...)
		var env out.Envelope
		if err := json.Unmarshal([]byte(stdout), &env); err != nil {
			t.Fatalf("%s: %v %q", args, err, stdout)
		}
		if env.Error == nil || env.Error.Code != "usage" || env.Command != want {
			t.Fatalf("%s: command %q, want %q: %s", args, env.Command, want, stdout)
		}
	}
}

func TestUnknownCommandPicks(t *testing.T) {
	code, stdout, _ := run(t, "ad", "add", "--json")
	if code != out.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil || env.Error.Code != "usage" || env.Error.Message != `unknown command "ad"` || !slices.Contains(env.Error.Candidates, "add") {
		t.Fatalf("unexpected envelope %+v", env)
	}
	_, _, stderr := run(t, "ad", "add")
	if strings.Contains(stderr, "Usage") {
		t.Fatalf("an unknown command with picks printed usage:\n%s", stderr)
	}
	for _, want := range []string{"did you mean:", "\u2023 add", "For example:", "$ shulker add\n"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestUnknownCommandHuman(t *testing.T) {
	code, stdout, stderr := run(t, "bogus")
	if code != out.ExitUsage || stdout != "" || !strings.HasPrefix(stderr, "  ✘ Unknown command") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestProjectFileCodes(t *testing.T) {
	dir := t.TempDir()
	if code, stdout, _ := run(t, "build", "-C", dir, "--json"); code != out.ExitError || failureCode(t, stdout).Code != "manifest-not-found" {
		t.Fatalf("no shulker.json: exit %d %s", code, stdout)
	}
	if err := os.WriteFile(filepath.Join(dir, "shulker.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := run(t, "build", "-C", dir, "--json"); code != out.ExitError || failureCode(t, stdout).Code != "manifest-invalid" {
		t.Fatalf("invalid shulker.json: exit %d %s", code, stdout)
	}
}
