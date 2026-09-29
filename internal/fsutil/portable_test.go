package fsutil

import "testing"

func TestIsPortableLocal(t *testing.T) {
	for _, rel := range []string{"mods/a.jar", "config/console.txt", "mods/com10.jar", "mods/..a.jar", ".hidden/x", "a\\b", "x/./y", "mods/*.jar"} {
		if !IsPortableLocal(rel) {
			t.Errorf("%s should be portable", rel)
		}
	}
	for _, rel := range []string{"", "../x", "a/../../b", "..", "/etc/x", `\x`, `\\server\x`, "C:/x", "c:x", "a/b:c", "CON", "mods/nul.jar", "COM1/x", "mods/lpt9.txt", "mods/Aux .jar", "PRN.txt.bak"} {
		if IsPortableLocal(rel) {
			t.Errorf("%s should not be portable", rel)
		}
	}
}
