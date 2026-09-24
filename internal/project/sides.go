package project

import (
	"slices"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// NoSide is what a local-project command raises when the manifest declares no block for the side
// it needs; its user can add the block.
func NoSide(side string) error {
	e := out.Errorf("no-side", "%s declares no %s", manifest.FileName, side)
	e.Rows = []out.Detail{{Label: "Fix", Text: `add "` + side + `": {} to ` + manifest.FileName}}
	return e
}

// AmbiguousSide is what a command that works on one side raises when the manifest declares both
// and nothing chose between them. flag names the flag that would, or is empty when the side is
// positional.
func AmbiguousSide(sides []string, flag string) error {
	e := out.Errorf("ambiguous-side", "%s declares both sides", manifest.FileName)
	e.Help = "choose one"
	e.Candidates, e.Flag = sides, flag
	return e
}

// NoClient is what a command that can take a remote source raises when the manifest declares no
// client; its user may not own the manifest, so the fix is the flag.
func NoClient() error {
	e := out.Errorf("no-side", "%s declares no client", manifest.FileName)
	e.Rows = []out.Detail{{Label: "Fix", Text: "pass --assume-client to build one anyway"}}
	return e
}

func checkSide(name, flag string) error {
	if manifest.IsSide(name) {
		return nil
	}
	e := manifest.NotASide(name)
	e.Flag = flag
	return e
}

// DeclaredSide is the side name, once it is a side the manifest declares; flag names the flag it
// came from, or is empty when it was positional.
func DeclaredSide(m *manifest.Manifest, name, flag string) (string, error) {
	if err := checkSide(name, flag); err != nil {
		return "", err
	}
	if !m.HasSide(name) {
		return "", NoSide(name)
	}
	return name, nil
}

// Sides is every side the manifest declares, or only the one named when name is given.
func Sides(m *manifest.Manifest, name string) ([]string, error) {
	if name == "" {
		return m.Sides(), nil
	}
	side, err := DeclaredSide(m, name, "")
	if err != nil {
		return nil, err
	}
	return []string{side}, nil
}

// SingleSide is the side a command that works on one side takes: the one --side wants, or the only
// one the manifest declares.
func SingleSide(m *manifest.Manifest, want string) (string, error) {
	if want != "" {
		return DeclaredSide(m, want, "--side")
	}
	sides := m.Sides()
	if len(sides) == 1 {
		return sides[0], nil
	}
	return "", AmbiguousSide(sides, "--side")
}

// ClientSide is the client side, declared or assumed; assumed reports that the manifest declares
// none and the command builds one from the shared mods and overrides anyway.
func ClientSide(m *manifest.Manifest, assume bool) (side string, assumed bool, err error) {
	if m.HasSide("client") {
		return "client", false, nil
	}
	if !assume {
		return "", false, NoClient()
	}
	return "client", true, nil
}

// SyncSide is the side a sync builds: the assumed client when the manifest declares none and the
// command says to assume one, else the side --side wants or the only one declared.
func SyncSide(m *manifest.Manifest, want string, assume bool) (side string, assumed bool, err error) {
	if m.HasSide("client") {
		assume = false
	}
	switch {
	case assume && want == "server":
		return "", false, out.Errorf("usage", "--assume-client builds the client; it doesn't go with --side server")
	case assume:
		return ClientSide(m, true)
	case want == "client" && !m.HasSide("client"):
		return "", false, NoClient()
	}
	side, err = SingleSide(m, want)
	return side, false, err
}

// ExportSides is the sides an export packs: every declared one, with a client assumed in front
// when the manifest declares none and the command says to assume one, or only the side --side
// wants when it is among them.
func ExportSides(m *manifest.Manifest, want string, assume bool) (sides []string, assumed bool, err error) {
	sides = m.Sides()
	if assume && !m.HasSide("client") {
		assumed = true
		sides = append([]string{"client"}, sides...)
	}
	if want == "" {
		return sides, assumed, nil
	}
	if err := checkSide(want, "--side"); err != nil {
		return nil, false, err
	}
	if !slices.Contains(sides, want) {
		if want == "client" {
			return nil, false, NoClient()
		}
		return nil, false, NoSide(want)
	}
	return []string{want}, assumed, nil
}
