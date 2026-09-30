package out

import "testing"

func TestShellArgQuotesForTheUsersShell(t *testing.T) {
	for _, c := range []struct{ in, goos, want string }{
		{"main", "linux", "main"},
		{"/home/a/My Pack", "linux", "'/home/a/My Pack'"},
		{"/home/a/it's", "darwin", `'/home/a/it'\''s'`},
		{`D:\shulker test\other drive`, "windows", `"D:\shulker test\other drive"`},
		{`C:\Users\a\shulker.exe.old`, "windows", `"C:\Users\a\shulker.exe.old"`},
	} {
		if got := shellArg(c.in, c.goos); got != c.want {
			t.Errorf("shellArg(%q, %s) = %s, want %s", c.in, c.goos, got, c.want)
		}
	}
}
