package resolve

import "shulker.sh/shulker/internal/out"

// MatchProblem is the one problem an ignore names, or nil when the locked mods
// have none between the pair. Without a rule the pair is ambiguous when it has
// both a depends and a breaks problem.
func MatchProblem(problems []Problem, mod, on, rule string) (*Problem, error) {
	var matches []Problem
	for _, p := range problems {
		if p.Mod == mod && p.On == on && (rule == "" || p.Rule == rule) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	}
	e := out.Errorf("usage", "%s on %s has both a depends and a breaks problem", mod, on)
	e.Help = "pass --rule depends or --rule breaks"
	return nil, e
}

// NoProblem is the error for an ignore that names no current problem, offering
// the pairs that do have one.
func NoProblem(problems []Problem, mod, on string) error {
	var pairs, ons []string
	for _, p := range problems {
		pairs = append(pairs, p.Mod+" "+p.On)
		if p.Mod == mod {
			ons = append(ons, p.On)
		}
	}
	e := out.Errorf("no-problem", "the locked mods have no problem between %s and %s", mod, on)
	e.Help = "pass --rule and --declared from the failed command"
	if len(problems) == 0 {
		return e
	}
	e.Help += ", or pick a current problem"
	e.Candidates = pairs
	if len(ons) > 0 {
		e.Candidates, e.Given = ons, on
	}
	return e
}
