package cli

import (
	"errors"

	"shulker.sh/shulker/internal/out"
)

// canPick reports whether shulker can ask. A picker reads keys and redraws, so stdin and the
// stream it draws on both have to be terminals.
func (a *app) canPick() bool {
	return a.tty != nil && a.tty() && a.printer.CanPick()
}

// pickOne asks which of several things a command's argument meant. A run that can't draw a picker
// and one where the picker is escaped both land on ambiguous, because the matches and how to name
// one without being asked again are what the caller needs either way.
func pickOne[T any](a *app, title string, matches []T, id func(T) string, label func(T) string, ambiguous func() error) (T, error) {
	var none T
	if !a.canPick() {
		return none, ambiguous()
	}
	choices := make([]out.Choice, len(matches))
	for i, m := range matches {
		choices[i] = out.Choice{Label: label(m), Value: id(m)}
	}
	chosen, err := a.printer.Pick(title, choices, a.stdin)
	if errors.Is(err, out.ErrPickCancelled) {
		return none, ambiguous()
	}
	if err != nil {
		return none, err
	}
	for _, m := range matches {
		if id(m) == chosen {
			return m, nil
		}
	}
	return none, ambiguous()
}
