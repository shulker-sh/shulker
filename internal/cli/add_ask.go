package cli

import (
	"context"
	"errors"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

// addAsking is r.Add with the two refusals a terminal can answer turned into questions: which
// loader a project with none should use, and whether to move a version the lock holds.
func (a *app) addAsking(ctx context.Context, r *resolve.Resolver, slug string, opts resolve.AddOptions) error {
	if !a.asksYes() {
		return r.Add(ctx, slug, opts)
	}
	r.AskMove = a.askMove
	err := r.Add(ctx, slug, opts)
	if out.CodeOf(err) != "loader-required" || !a.canPick() {
		return err
	}
	a.printer.Settle()
	name, pickErr := a.questions().Pick("Which mod loader?", loaderChoices(), a.stdin)
	if errors.Is(pickErr, out.ErrPickCancelled) {
		return err
	}
	if pickErr != nil {
		return pickErr
	}
	r.Manifest.Loader = manifest.Loader{Type: name}
	if _, err := r.Reconcile(ctx); err != nil {
		return err
	}
	return r.Add(ctx, slug, opts)
}

// askMove prints the refusal without its nudge, then asks the nudge's lead back as the question.
func (a *app) askMove(held *out.Error) (bool, error) {
	if a.yes {
		return true, nil
	}
	a.printer.Settle()
	shown := *held
	shown.Nudge = out.Nudge{}
	a.printer.Err().Error(&shown)
	return a.askYes(held.Nudge.Lead + "?")
}
