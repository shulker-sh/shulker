package cli

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestArgumentErrorsListWhatIsWrong(t *testing.T) {
	cases := []struct {
		args    []string
		message string
		items   []string
	}{
		{[]string{"config", "set"}, "missing 2 arguments", []string{"<key>", "<value>"}},
		{[]string{"config", "set", "registry"}, "missing an argument", []string{"<value>"}},
		{[]string{"add"}, "missing at least one argument", []string{"<mod|source>"}},
		{[]string{"export", "mrpack", "a", "b", "c"}, "unexpected arguments", []string{"b", "c"}},
		{[]string{"version", "extra"}, "unexpected argument", []string{"extra"}},
	}
	for _, c := range cases {
		code, stdout, _ := run(t, append([]string{"--json"}, c.args...)...)
		e := failureCode(t, stdout)
		if code != out.ExitUsage || e.Code != "usage" || e.Message != c.message || !slices.Equal(e.Items, c.items) {
			t.Errorf("%v: exit %d, %+v", c.args, code, e)
		}
	}
	_, _, stderr := run(t, "config", "set")
	if !strings.HasPrefix(stderr, "  ✘ Missing 2 arguments (usage)\n    ├─ <key>\n    ├─ <value>\n    ├─ usage: shulker config set <key> <value> [flags]\n") {
		t.Fatalf("stderr:\n%s", stderr)
	}
}

func TestFlagErrorsAreReworded(t *testing.T) {
	cases := map[string][]string{
		"unknown flag --bogus":                     {"add", "sodium", "--bogus"},
		"unknown flag -z":                          {"-z"},
		"--side needs a value":                     {"add", "sodium", "--side"},
		`--check takes true or false, not "maybe"`: {"self", "update", "--check=maybe"},
		`can't read "---x" as a flag`:              {"---x"},
		"--without-attestation and --require-attestation can't be used together": {"self", "update", "--without-attestation", "--require-attestation"},
	}
	for message, args := range cases {
		code, stdout, _ := run(t, append([]string{"--json"}, args...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || e.Message != message {
			t.Errorf("%v: exit %d, %+v", args, code, e)
		}
	}
}

func TestScopeFlagsBelongToTheCommandsThatReadThem(t *testing.T) {
	cases := map[string][]string{
		"unknown flag -C":         {"version", "-C", "elsewhere"},
		"unknown flag -i":         {"init", "-i", "nosuch"},
		"unknown flag --dir":      {"instances", "--dir", "elsewhere"},
		"unknown flag --instance": {"cache", "info", "--instance", "nosuch"},
		"unknown flag --id":       {"accounts", "--id", "nosuch"},
	}
	for message, args := range cases {
		code, stdout, _ := run(t, append([]string{"--json"}, args...)...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || e.Message != message {
			t.Errorf("%v: exit %d, %+v", args, code, e)
		}
	}
	root := newApp(nil, nil).root()
	for _, c := range []struct {
		path          []string
		dir, instance bool
	}{
		{[]string{"list"}, true, true},
		{[]string{"sync"}, true, true},
		{[]string{"feature", "on"}, true, true},
		{[]string{"hook", "pre-launch"}, true, true},
		{[]string{"init"}, true, false},
		{[]string{"cache", "prune"}, true, false},
		{[]string{"log"}, false, true},
		{[]string{"version"}, false, false},
		{[]string{"accounts"}, false, false},
	} {
		cmd, _, err := root.Find(c.path)
		if err != nil {
			t.Fatal(err)
		}
		dir, instance := cmd.Flags().Lookup("dir") != nil, cmd.Flags().Lookup("instance") != nil
		if dir != c.dir || instance != c.instance {
			t.Errorf("%v: -C %t, -i %t", c.path, dir, instance)
		}
	}
}
