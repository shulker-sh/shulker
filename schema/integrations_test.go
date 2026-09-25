package schema

import (
	"encoding/json"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/integrations"
)

func TestIntegrationIDsMatchTheSchema(t *testing.T) {
	raw, err := files.ReadFile(string(Manifest))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs struct {
			Integration struct {
				Enum []string `json:"enum"`
			} `json:"integration"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if got, want := slices.Sorted(slices.Values(doc.Defs.Integration.Enum)), slices.Sorted(slices.Values(integrations.IDs())); !slices.Equal(got, want) {
		t.Fatalf("the schema's integration ids are %v, integrations has %v", got, want)
	}
}
