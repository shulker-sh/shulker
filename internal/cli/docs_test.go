package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var (
	docsHeading = regexp.MustCompile("^### `(shulker[^`]*)`")
	// Reading names from FlagUsages avoids importing pflag, which go.mod
	// only lists as indirect.
	usageFlag   = regexp.MustCompile(`(?m)^\s+(?:-\w, )?--([\w-]+)`)
	docsCodeRow = regexp.MustCompile("(?m)^\\| `([a-z0-9-]+)` \\|")
)

func TestCLIReferenceCoversEveryCommand(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "site", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	sections := docsSections(string(data))
	var missing []string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Hidden {
			return
		}
		if cmd.HasSubCommands() {
			for _, sub := range cmd.Commands() {
				visit(sub)
			}
			return
		}
		path := cmd.CommandPath()
		body, ok := sections[path]
		if !ok {
			missing = append(missing, path)
			return
		}
		for _, m := range usageFlag.FindAllStringSubmatch(cmd.NonInheritedFlags().FlagUsages(), -1) {
			name := m[1]
			if name != "help" && !regexp.MustCompile(`--`+name+`([^\w-]|$)`).MatchString(body) {
				missing = append(missing, path+" --"+name)
			}
		}
	}
	visit(newApp(io.Discard, io.Discard).root())
	if len(missing) > 0 {
		t.Fatalf("site/docs/cli.md has no section or flag row for:\n  %s", strings.Join(missing, "\n  "))
	}
}

// docsSections maps each command path to the text under its heading. A
// heading like `shulker feature on|off` documents both commands.
func docsSections(doc string) map[string]string {
	sections := map[string]string{}
	var paths []string
	var body strings.Builder
	flush := func() {
		for _, p := range paths {
			sections[p] = body.String()
		}
		body.Reset()
	}
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			paths = nil
			if m := docsHeading.FindStringSubmatch(line); m != nil {
				paths = expandAlternatives(m[1])
			}
			continue
		}
		body.WriteString(line + "\n")
	}
	flush()
	return sections
}

func expandAlternatives(heading string) []string {
	words := strings.Fields(heading)
	prefix := strings.Join(words[:len(words)-1], " ")
	var paths []string
	for _, alt := range strings.Split(words[len(words)-1], "|") {
		paths = append(paths, prefix+" "+alt)
	}
	return paths
}

func TestJSONReferenceCoversEveryErrorCode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "site", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, m := range docsCodeRow.FindAllStringSubmatch(docsSection(string(data), "### Error codes"), -1) {
		documented[m[1]] = true
	}
	used := errorCodes(t, filepath.Join("..", ".."))
	var problems []string
	for code := range used {
		if !documented[code] {
			problems = append(problems, "no row for "+code)
		}
	}
	for code := range documented {
		if !used[code] {
			problems = append(problems, "row for unused "+code)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("site/docs/cli.md error codes are out of date:\n  %s", strings.Join(problems, "\n  "))
	}
}

// errorCodes finds the string literals passed as the code of out.Errorf or
// schema.Invalid, or set as a Code or code field, which is how every error
// code is written.
func errorCodes(t *testing.T, root string) map[string]bool {
	codes := map[string]bool{}
	fset := token.NewFileSet()
	walk := func(dir string) error {
		return filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				var lit ast.Expr
				switch n := n.(type) {
				case *ast.CallExpr:
					if isErrorf(n.Fun) && len(n.Args) > 0 {
						lit = n.Args[0]
					}
				case *ast.KeyValueExpr:
					if key, ok := n.Key.(*ast.Ident); ok && (key.Name == "Code" || key.Name == "code") {
						lit = n.Value
					}
				}
				if s, ok := lit.(*ast.BasicLit); ok && s.Kind == token.STRING {
					code, _ := strconv.Unquote(s.Value)
					codes[code] = true
				}
				return true
			})
			return nil
		})
	}
	// schema/ owns schema-newer, the one code born outside internal/.
	for _, dir := range []string{"internal", "schema"} {
		if err := walk(dir); err != nil {
			t.Fatal(err)
		}
	}
	return codes
}

func isErrorf(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && (pkg.Name == "out" && sel.Sel.Name == "Errorf" || pkg.Name == "schema" && sel.Sel.Name == "Invalid")
}

func docsSection(doc, heading string) string {
	_, rest, _ := strings.Cut(doc, "\n"+heading+"\n")
	if i := strings.Index(rest, "\n#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}
