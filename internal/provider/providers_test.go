package provider_test

import (
	"errors"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

func twoProviders() provider.Providers {
	alpha, beta := fake.New("alpha"), fake.New("beta")
	beta.Unavailable = out.Errorf("provider-unavailable", "beta needs a key")
	return provider.Providers{"alpha": alpha, "beta": beta}
}

func TestParseURLAsksEachProvider(t *testing.T) {
	ps := twoProviders()
	ref, ok, err := ps.ParseURL("https://beta.test/mod/shiny/version/2")
	if err != nil || !ok || ref != (provider.Ref{Provider: "beta", Project: "shiny", Version: "2"}) {
		t.Fatalf("got %+v ok=%v err=%v", ref, ok, err)
	}
	for _, arg := range []string{"shiny", "./shiny.jar", "https://example.test/mod/shiny"} {
		if _, ok, err := ps.ParseURL(arg); ok || err != nil {
			t.Errorf("%s: ok=%v err=%v, want it left alone", arg, ok, err)
		}
	}
	_, _, err = ps.ParseURL("https://alpha.test/mods")
	var e *out.Error
	if !errors.As(err, &e) || e.Code != "usage" || !slices.Equal(e.Items, []string{"https://alpha.test/<kind>/<slug>[/version/<id>]", "https://beta.test/<kind>/<slug>[/version/<id>]"}) {
		t.Fatalf("unreadable shape: %v", err)
	}
}

func TestGetSaysWhyAProviderCantBeUsed(t *testing.T) {
	ps := twoProviders()
	if _, err := ps.Get("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Get("beta"); out.CodeOf(err) != "provider-unavailable" || out.AsError(err).Message != "beta needs a key" {
		t.Errorf("beta: %v", err)
	}
	if _, err := ps.Get("gamma"); out.CodeOf(err) != "provider-unavailable" || out.AsError(err).Message != "gamma is not a known provider" {
		t.Errorf("gamma: %v", err)
	}
	if ps.Title("alpha") != "Alpha" || ps.Title("gamma") != "gamma" {
		t.Errorf("titles %s %s", ps.Title("alpha"), ps.Title("gamma"))
	}
}
