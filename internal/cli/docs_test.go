package cli

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var (
	docsHeading = regexp.MustCompile("^### `(shulker[^`]*)`")
	// Reading names from FlagUsages avoids importing pflag, which go.mod
	// only lists as indirect.
	usageFlag = regexp.MustCompile(`(?m)^\s+(?:-\w, )?--([\w-]+)`)
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
