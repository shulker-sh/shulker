package sandbox

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// deniedRoots are where a Mac keeps what a game has no business with: every home, and every other
// volume. Denying a home alone leaves another volume readable.
var deniedRoots = []string{"/Users", "/Volumes"}

// alwaysWritable are the folders every process unpacks and caches into: LWJGL, JNA and Netty
// extract there, and the per-user Metal and GL shader caches live there.
var alwaysWritable = []string{"/private/tmp", "/private/var/folders"}

// Profile is the sandbox-exec profile for a policy. It allows by default and rules on files alone:
// a deny-default profile would have to list every Mach service, IOKit class and sysctl the window,
// the GPU and the audio need. A later rule wins over an earlier one on the same operation, which
// is why each allow names the operation of the deny it overrides.
func Profile(p Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n\n")
	rule := func(action, operations string, filters []string) {
		if len(filters) > 0 {
			fmt.Fprintf(&b, "(%s %s\n  %s)\n", action, operations, strings.Join(filters, "\n  "))
		}
	}
	rule("deny", "file-read* file-write*", subpaths(deniedRoots))
	// Java resolves its own path by walking up from bin/java, and dies before printing anything if
	// a folder on the way can't be stat'ed. A literal leaks no listing.
	rule("allow", "file-read-metadata", literals(ancestors(slices.Concat(p.Read, p.ReadFiles, p.Write))))
	rule("allow", "file-read*", slices.Concat(subpaths(p.Read), subpaths(p.Write), literals(p.ReadFiles)))
	b.WriteString("\n")
	rule("deny", "file-write*", subpaths([]string{"/"}))
	rule("allow", "file-write*", slices.Concat(subpaths(p.Write), subpaths(alwaysWritable)))
	b.WriteString(`(allow file-write* (literal "/dev/null") (literal "/dev/tty") (literal "/dev/dtracehelper") (regex #"^/dev/fd/"))` + "\n\n")
	rule("deny", "file-write*", slices.Concat(subpaths(p.Protect), literals(p.ProtectFiles)))
	rule("allow", "file-write*", subpaths(p.Reopen))
	return b.String()
}

// ancestors are the folders above paths that sit inside a denied root, down from the root itself.
func ancestors(paths []string) []string {
	var dirs []string
	for _, path := range paths {
		for _, root := range deniedRoots {
			if !within(path, root) {
				continue
			}
			for dir := filepath.Dir(path); within(dir, root); dir = filepath.Dir(dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	slices.Sort(dirs)
	return slices.Compact(dirs)
}

func subpaths(paths []string) []string { return filters("subpath", paths) }
func literals(paths []string) []string { return filters("literal", paths) }

func filters(kind string, paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = fmt.Sprintf("(%s %s)", kind, quote(path))
	}
	return out
}

// quote writes a path as an SBPL string.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
