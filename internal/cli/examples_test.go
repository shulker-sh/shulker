package cli

import (
	"strings"
	"testing"
)

func TestErrorsShowAnExampleCommand(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	fresh := newHarness(t)

	for _, tc := range []struct {
		h    *harness
		args []string
		want []string
	}{
		{h, []string{"remove", "sodim"}, []string{"did you mean:", "‣ sodium", "For example:", "$ shulker remove sodium\n"}},
		{h, []string{"set", "sever.eula", "true"}, []string{"did you mean:", "‣ server", "$ shulker set server.eula true\n"}},
		{fresh, []string{"create", "--name", "pack", "--side", "clint"}, []string{"did you mean:", "‣ client", "$ shulker create --name pack --side client\n"}},
	} {
		code, _, stderr := tc.h.run(t, tc.args...)
		if code == 0 {
			t.Fatalf("%v succeeded", tc.args)
		}
		for _, w := range tc.want {
			if !strings.Contains(stderr, w) {
				t.Errorf("%v: missing %q in:\n%s", tc.args, w, stderr)
			}
		}
	}
}
