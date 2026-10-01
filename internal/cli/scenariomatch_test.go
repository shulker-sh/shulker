package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// checkFragments checks a step's --json output: expect must be contained in it and reject must
// not, each a JSON fragment, empty for none. A reject naming a key the output never has fails
// too, since it would pass whatever the command did.
func checkFragments(expect, reject, output string) error {
	var actual any
	if err := json.Unmarshal([]byte(output), &actual); err != nil {
		return fmt.Errorf("output is not JSON: %v\n%s", err, output)
	}
	if expect != "" {
		fragment, err := decodeFragment(expect)
		if err != nil {
			return err
		}
		if !contains(fragment, actual) {
			return fmt.Errorf("expect not matched: %s\noutput: %s", compactJSON(expect), output)
		}
	}
	if reject != "" {
		fragment, err := decodeFragment(reject)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		collectKeys(actual, have)
		want := map[string]bool{}
		collectKeys(fragment, want)
		var typos []string
		for key := range want {
			if !have[key] {
				typos = append(typos, key)
			}
		}
		if len(typos) > 0 {
			slices.Sort(typos)
			return fmt.Errorf("reject names %s, which the output never has: likely a typo\nreject: %s\noutput: %s", strings.Join(typos, ", "), compactJSON(reject), output)
		}
		if contains(fragment, actual) {
			return fmt.Errorf("reject matched: %s\noutput: %s", compactJSON(reject), output)
		}
	}
	return nil
}

func decodeFragment(raw string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("fragment %s: %v", raw, err)
	}
	return v, nil
}

func compactJSON(raw string) string {
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(raw)) != nil {
		return raw
	}
	return buf.String()
}

// contains reports whether actual holds fragment: an object the keys it names, an array each
// listed element in a different element of its own, and anything else an equal value.
func contains(fragment, actual any) bool {
	switch f := fragment.(type) {
	case map[string]any:
		a, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, fv := range f {
			av, ok := a[key]
			if !ok || !contains(fv, av) {
				return false
			}
		}
		return true
	case []any:
		a, ok := actual.([]any)
		return ok && matchAll(f, a)
	default:
		return fragment == actual
	}
}

// matchAll finds a matching that gives each fragment element its own actual element, by
// augmenting paths, since a greedy pick can take the one element a later fragment needed.
func matchAll(fragments, actual []any) bool {
	owner := make([]int, len(actual))
	for i := range owner {
		owner[i] = -1
	}
	var place func(f int, seen []bool) bool
	place = func(f int, seen []bool) bool {
		for i, a := range actual {
			if seen[i] || !contains(fragments[f], a) {
				continue
			}
			seen[i] = true
			if owner[i] == -1 || place(owner[i], seen) {
				owner[i] = f
				return true
			}
		}
		return false
	}
	for f := range fragments {
		if !place(f, make([]bool, len(actual))) {
			return false
		}
	}
	return true
}

func collectKeys(v any, keys map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for key, child := range v {
			keys[key] = true
			collectKeys(child, keys)
		}
	case []any:
		for _, child := range v {
			collectKeys(child, keys)
		}
	}
}

func TestFragmentMatchesNamedKeysAtAnyDepth(t *testing.T) {
	output := `{"ok":true,"data":{"added":[{"slug":"sodium","side":"client"}],"count":1}}`
	if err := checkFragments(`{"data":{"added":[{"slug":"sodium"}]}}`, "", output); err != nil {
		t.Fatal(err)
	}
	if err := checkFragments(`{"data":{"added":[{"slug":"iris"}]}}`, "", output); err == nil {
		t.Fatal("a missing element matched")
	}
	if err := checkFragments(`{"data":{"count":2}}`, "", output); err == nil {
		t.Fatal("a different number matched")
	}
	if err := checkFragments(`{"data":{"added":{"slug":"sodium"}}}`, "", output); err == nil {
		t.Fatal("an object matched an array")
	}
}

func TestFragmentArraysMatchAsAMultiset(t *testing.T) {
	output := `{"data":{"added":[{"slug":"iris","side":"client"},{"slug":"sodium","side":"client"},{"slug":"fabric-api","side":"both"}]}}`
	if err := checkFragments(`{"data":{"added":[{"slug":"sodium"},{"slug":"iris"}]}}`, "", output); err != nil {
		t.Fatalf("order mattered: %v", err)
	}
	if err := checkFragments(`{"data":{"added":[{"side":"client"},{"side":"client"}]}}`, "", output); err != nil {
		t.Fatalf("two client elements: %v", err)
	}
	if err := checkFragments(`{"data":{"added":[{"side":"both"},{"side":"both"}]}}`, "", output); err == nil {
		t.Fatal("one element matched two listed ones")
	}
	// A greedy match takes iris for the first fragment and leaves nothing for the second.
	if err := checkFragments(`{"data":{"added":[{"side":"client"},{"slug":"iris"}]}}`, "", output); err != nil {
		t.Fatalf("a matching was missed: %v", err)
	}
}

func TestRejectListingAnElementTwiceSaysNotTwice(t *testing.T) {
	once := `{"data":{"added":[{"slug":"sodium"},{"slug":"iris"}]}}`
	twice := `{"data":{"added":[{"slug":"sodium"},{"slug":"sodium"}]}}`
	reject := `{"data":{"added":[{"slug":"sodium"},{"slug":"sodium"}]}}`
	if err := checkFragments("", reject, once); err != nil {
		t.Fatalf("once was rejected: %v", err)
	}
	err := checkFragments("", reject, twice)
	if err == nil {
		t.Fatal("twice passed")
	}
	if !strings.Contains(err.Error(), reject) || !strings.Contains(err.Error(), twice) {
		t.Fatalf("the failure names neither the fragment nor the output: %v", err)
	}
}

func TestRejectWithAKeyNowhereInTheOutputIsATypo(t *testing.T) {
	output := `{"ok":true,"data":{"added":[{"slug":"sodium"}],"problems":[]}}`
	if err := checkFragments("", `{"data":{"problems":[{}]}}`, output); err != nil {
		t.Fatalf("an empty list was rejected: %v", err)
	}
	err := checkFragments("", `{"data":{"problmes":[{}]}}`, output)
	if err == nil || !strings.Contains(err.Error(), "problmes") {
		t.Fatalf("a misspelt key passed: %v", err)
	}
}

func TestExpectFailureShowsTheFragmentAndTheOutput(t *testing.T) {
	output := `{"ok":false}`
	err := checkFragments(`{"ok":true}`, "", output)
	if err == nil || !strings.Contains(err.Error(), `{"ok":true}`) || !strings.Contains(err.Error(), output) {
		t.Fatalf("got %v", err)
	}
}
