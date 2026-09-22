package resolve

import (
	"errors"
	"fmt"

	"shulker.sh/shulker/internal/out"
)

// prefixed names where err came from. Wrapping an *out.Error with fmt.Errorf wouldn't: the error
// renders from the *out.Error inside, whose headline doesn't carry the prefix.
func prefixed(prefix string, err error) error {
	var e *out.Error
	if errors.As(err, &e) {
		e.Message = prefix + ": " + e.Message
		return err
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func providedBy(id, packs, help string) *out.Error {
	e := out.Errorf("modpack-provided", "%s is provided by modpack %s", id, packs)
	e.Help = help
	return e
}

func rangeInvalid(what, raw string, err error) *out.Error {
	e := out.Errorf("manifest-invalid", "%s %q isn't a version range shulker can read", what, raw)
	e.Rows = []out.Detail{{Label: what, Text: err.Error()}}
	return e
}
