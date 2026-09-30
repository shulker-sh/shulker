package out

import "golang.org/x/sys/windows"

// legacyCodePage reports whether piped output would be decoded in a console code page that isn't
// UTF-8, as cmd's more and PowerShell's pipeline do, which turns every non-ASCII glyph into
// mojibake. With no console at all the output goes to a file, which reads as UTF-8.
func legacyCodePage(tty bool) bool {
	if tty {
		return false
	}
	cp, err := windows.GetConsoleOutputCP()
	return err == nil && cp != 0 && cp != 65001
}
