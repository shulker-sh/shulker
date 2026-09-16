package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
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
	lines := strings.Split(stdout, "\n")
	if len(lines) < 5 || lines[0] != "" || !strings.HasPrefix(lines[1], "  shulker dev") || lines[2] != "" {
		t.Fatalf("unexpected output %q", stdout)
	}
	for _, label := range []string{"Go        go", "Binary    ", "Config    ", "Cache     "} {
		if !strings.Contains(stdout, "\n  "+label) {
			t.Fatalf("missing %q row in %q", strings.TrimSpace(label), stdout)
		}
	}
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

func TestHomeTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if got := homeTilde(filepath.Join(home, "x", "y")); got != "~"+string(filepath.Separator)+filepath.Join("x", "y") {
		t.Fatalf("homeTilde = %q", got)
	}
	if got := homeTilde(home + "-other"); got != home+"-other" {
		t.Fatalf("homeTilde = %q", got)
	}
}

func TestHelpLinksDocs(t *testing.T) {
	for args, url := range map[string]string{
		"--help":            docsURL,
		"add --help":        docsURL + "/cli#shulker-add",
		"target add --help": docsURL + "/cli#shulker-target-add",
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
	if strings.Contains(stdout, "(default: true)") || strings.Contains(stdout, "More:") {
		t.Fatalf("add help:\n%s", stdout)
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
	if _, stdout, _ := run(t, "__complete", "target", "l"); !strings.Contains(stdout, "list") {
		t.Fatalf("__complete target l = %q", stdout)
	}
}

func TestHelpCommandReportsUnknownTopics(t *testing.T) {
	for args, want := range map[string]string{
		"help nosuch":     `unknown command "nosuch"`,
		"help target lst": `unknown command "lst" for "shulker target"`,
	} {
		code, stdout, _ := run(t, append([]string{"--json"}, strings.Fields(args)...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || e.Message != want {
			t.Errorf("%s: exit %d, %+v", args, code, e)
		}
	}
	if _, _, stderr := run(t, "help", "target", "lst"); !strings.Contains(stderr, "$ shulker help target list\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	for help, flag := range map[string]string{"help add": "add --help", "help rm": "remove --help", "help target list": "target list --help"} {
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
	for _, args := range [][]string{{"target", "lst"}, {"self", "updte"}, {"config", "st", "key"}} {
		code, stdout, _ := run(t, append([]string{"--json"}, args...)...)
		e := failureCode(t, stdout)
		want := `unknown command "` + args[1] + `" for "shulker ` + args[0] + `"`
		if code != out.ExitUsage || e.Code != "usage" || e.Message != want || len(e.Candidates) == 0 {
			t.Errorf("%v: exit %d, %+v", args, code, e)
		}
	}
	_, _, stderr := run(t, "target", "lst")
	for _, want := range []string{"did you mean:", "\u2023 list", "$ shulker target list\n"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if code, stdout, _ := run(t, "target"); code != out.ExitOK || !strings.Contains(stdout, "  Commands\n") {
		t.Fatalf("target alone: exit %d\n%s", code, stdout)
	}
}

func TestUsageErrorsShowUsageAndFlags(t *testing.T) {
	_, _, stderr := run(t, "add")
	if !strings.Contains(stderr, "\n\n  Usage\n    $ shulker add <mod|source>... [flags]\n\n  Flags\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "Global flags") || strings.Contains(stderr, "Examples") || strings.HasSuffix(stderr, "\n\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if _, _, stderr := run(t, "player"); !strings.Contains(stderr, "$ shulker player") || !strings.Contains(stderr, "--all") {
		t.Fatalf("player stderr:\n%s", stderr)
	}
	if _, stdout, stderr := run(t, "add", "--json"); stderr != "" || strings.Contains(stdout, "Usage") {
		t.Fatalf("add --json stdout %q stderr %q", stdout, stderr)
	}
}

func TestWrapWordsKeepsCodeSpansPaired(t *testing.T) {
	lines := wrapWords("run `shulker lock --force now` to fix it", 18)
	for _, line := range lines {
		if strings.Count(line, "`")%2 != 0 || out.Width(strings.ReplaceAll(line, "`", "")) > 18 {
			t.Fatalf("lines %q", lines)
		}
	}
	if strings.Join(lines, " ") != "run `shulker lock` `--force now` to fix it" {
		t.Fatalf("lines %q", lines)
	}
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
	if !strings.HasPrefix(stderr, "  ✘ error: --side takes one of client, server, both, not \"top\" (usage)\n\n  Usage\n") {
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
	if code != out.ExitUsage || stdout != "" || !strings.HasPrefix(stderr, "  ✘ error: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{"My Pack": "my-pack", "tmp.KEnt9tWC1o": "tmp.kent9twc1o", "---": "shulker-project", "west_coast SMP!": "west_coast-smp"}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
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
