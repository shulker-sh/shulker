package out

import (
	"regexp"
	"runtime"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/near"
)

type pick struct {
	Show string
	Pass string
}

// picks is what a human error lists: the closest matches when the typed value
// is a near miss, otherwise every option when there are few enough to read.
func (e *Error) picks() (label string, picks []pick) {
	all := make([]pick, len(e.Candidates))
	passes := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		all[i] = pick{Show: c, Pass: c}
		if i < len(e.Pass) {
			all[i].Pass = e.Pass[i]
		}
		passes[i] = all[i].Pass
	}
	if e.Given == "" {
		return "pick one", all
	}
	if e.Code == "usage" && e.Flag != "" {
		// A bad flag value is a command-line mistake like any other, and its tree carries the
		// usage and help rows instead of suggested values.
		return "", nil
	}
	for _, hit := range near.Closest(e.Given, passes, 3) {
		picks = append(picks, all[slices.Index(passes, hit)])
	}
	if len(picks) > 0 {
		return "did you mean", picks
	}
	if len(all) <= 5 {
		return "pick one", all
	}
	return "", nil
}

// exampleCommand is the typed command line with value in place of the given
// argument, or set as the flag's value. A given argument typed twice can't be
// placed, so there is no example.
func exampleCommand(args []string, e *Error, value string) (string, bool) {
	words := slices.Clone(args)
	switch {
	case e.Given != "":
		at := -1
		for i, w := range words {
			if w == e.Given || (strings.HasPrefix(w, "-") && strings.HasSuffix(w, "="+e.Given)) {
				if at >= 0 {
					return "", false
				}
				at = i
			}
		}
		if at < 0 {
			return "", false
		}
		if words[at] == e.Given && slices.Contains(slices.Delete(slices.Clone(words), at, at+1), value) {
			words = slices.Delete(words, at, at+1)
			break
		}
		if flag, _, ok := strings.Cut(words[at], "="); ok && words[at] != e.Given {
			words[at] = flag + "=" + value
		} else {
			words[at] = value
		}
	case e.Flag != "":
		set := false
		for i := 0; i < len(words); i++ {
			switch {
			case words[i] == e.Flag && i+1 < len(words):
				words[i+1] = value
				set = true
			case strings.HasPrefix(words[i], e.Flag+"="):
				words[i] = e.Flag + "=" + value
				set = true
			}
		}
		if !set {
			words = append(words, e.Flag, value)
		}
	case e.Setting != "":
		words = []string{"set", e.Setting, value}
	default:
		return "", false
	}
	return CommandLine(words), true
}

// CommandLine is a shulker command with its arguments quoted for the shell.
func CommandLine(args []string) string {
	words := []string{"shulker"}
	for _, w := range args {
		words = append(words, shellQuote(w))
	}
	return strings.Join(words, " ")
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if plainArg.MatchString(s) {
		return s
	}
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
