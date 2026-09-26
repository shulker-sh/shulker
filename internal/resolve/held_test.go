package resolve

import (
	"context"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// heldProject locks fabric-api at 1.0.0 and only then lets a newer one exist, so anything that
// moves it has to be the add under test. needsNewAPI is whether sodium's jar wants the newer one.
func heldProject(t *testing.T, needsNewAPI bool) (*envtest.Host, *harness) {
	t.Helper()
	alpha := envtest.NewHost(envtest.NewCDN(t), "alpha")
	alpha.Publish(mod("a-fapi", "fabric-api"), provider.Version{Number: "1.0.0", File: provider.File{Filename: "fabric-api-1.0.0.jar"}}, modJar(t, "fabric-api", "1.0.0", "*"))
	h := newHarness(t, alpha)
	h.mustAdd("fabric-api", AddOptions{})
	alpha.Publish(mod("a-fapi", "fabric-api"), provider.Version{Number: "2.0.0", File: provider.File{Filename: "fabric-api-2.0.0.jar"}}, modJar(t, "fabric-api", "2.0.0", "*"))
	wants := "*"
	if needsNewAPI {
		wants = ">=2.0.0"
	}
	jar := fabricJar(t, `{"id":"sodium","version":"0.9.2","environment":"client","depends":{"fabricloader":">=0.17","fabric-api":"`+wants+`"}}`, nil)
	alpha.Publish(mod("a-sodium", "sodium"), provider.Version{Number: "0.9.2", File: provider.File{Filename: "sodium-0.9.2.jar"}, Dependencies: []provider.Dependency{dependsOn("a-fapi")}}, jar)
	return alpha, h
}

func TestAddHoldsTheVersionsTheLockAlreadyPins(t *testing.T) {
	_, h := heldProject(t, false)

	h.mustAdd("sodium", AddOptions{})

	if got := h.mod("fabric-api").VersionNumber; got != "1.0.0" {
		t.Fatalf("adding sodium should hold fabric-api at 1.0.0, got %s", got)
	}
}

func TestAddRefusesToMoveAHeldDependency(t *testing.T) {
	_, h := heldProject(t, true)

	err := h.add("sodium", AddOptions{})

	e := out.AsError(err)
	if e == nil || e.Code != "deps-held" {
		t.Fatalf("add should refuse to move a held dependency: %v", err)
	}
	if len(e.Items) != 1 || !strings.Contains(e.Items[0], "requires fabric-api >=2.0.0") || !strings.Contains(e.Items[0], "held at 1.0.0") {
		t.Fatalf("the refusal should name the held version and what needs it: %v", e.Items)
	}
	if got := h.mod("fabric-api").VersionNumber; got != "1.0.0" {
		t.Fatalf("a refused add should leave the held version alone, got fabric-api %s", got)
	}
}

func TestAddWithDepsMovesTheHeldDependency(t *testing.T) {
	_, h := heldProject(t, true)

	h.mustAdd("sodium", AddOptions{WithDeps: true})

	if got := h.mod("fabric-api").VersionNumber; got != "2.0.0" {
		t.Fatalf("with deps should move fabric-api, got %s", got)
	}
	if by := h.mod("fabric-api").RequiredBy; len(by) != 1 || by[0] != "sodium" {
		t.Fatalf("the moved dependency is still sodium's: %v", by)
	}
	if _, locked := h.r.Lock.Mods["sodium"]; !locked {
		t.Fatal("with deps should still add the mod that needed the move")
	}
}

func TestUpdateKeepsAPinnedDirectDependency(t *testing.T) {
	alpha, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	h.mustAdd("fabric-api", AddOptions{})
	ctx := context.Background()
	if _, err := h.r.Pin(ctx, "fabric-api", ""); err != nil {
		t.Fatal(err)
	}
	alpha.Publish(mod("a-fapi", "fabric-api"), provider.Version{Number: "2.0.0", File: provider.File{Filename: "fabric-api-2.0.0.jar"}}, modJar(t, "fabric-api", "2.0.0", "*"))
	alpha.Publish(mod("a-sodium", "sodium"), provider.Version{Number: "1.1.0", File: provider.File{Filename: "sodium-1.1.0.jar"}, Dependencies: []provider.Dependency{dependsOn("a-fapi")}}, modJar(t, "sodium", "1.1.0", "client"))

	before := h.r.Snapshot()
	if err := h.r.Update(ctx, []string{"sodium"}); err != nil {
		t.Fatal(err)
	}
	if c := h.r.Changes(before); len(c.Updated) != 1 || c.Updated[0].ID != "sodium" || c.Updated[0].To != "1.1.0" {
		t.Fatalf("update sodium: %+v", c)
	}
	if fa := h.mod("fabric-api"); fa.VersionNumber != "0.130.0" || len(fa.RequiredBy) != 1 || fa.RequiredBy[0] != "sodium" {
		t.Fatalf("fabric-api after update sodium: %+v", fa)
	}

	before = h.r.Snapshot()
	if err := h.r.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if c := h.r.Changes(before); !c.IsEmpty() {
		t.Fatalf("update all moves nothing: %+v", c)
	}
	if fa := h.mod("fabric-api"); fa.VersionNumber != "0.130.0" || len(fa.RequiredBy) != 1 {
		t.Fatalf("fabric-api after update all: %+v", fa)
	}
}
