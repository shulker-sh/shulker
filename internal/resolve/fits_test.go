package resolve

import (
	"context"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func TestFitsTheProjectsMinecraftAndLoader(t *testing.T) {
	_, _, h := twoHosts(t)
	ctx := context.Background()
	if fits, err := h.r.Fits(ctx, "alpha", "a-sodium", manifest.TypeMod); err != nil || !fits {
		t.Errorf("sodium on 26.2 fabric: fits %v, err %v", fits, err)
	}
	h.r.Lock.Loader.Type = "neoforge"
	if fits, err := h.r.Fits(ctx, "alpha", "a-sodium", manifest.TypeMod); err != nil || fits {
		t.Errorf("a fabric-only mod on neoforge: fits %v, err %v", fits, err)
	}
	if _, err := h.r.Fits(ctx, "gamma", "a-sodium", manifest.TypeMod); err == nil {
		t.Error("a provider the project has no adapter for fits")
	}
}
