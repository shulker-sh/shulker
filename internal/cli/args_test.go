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
