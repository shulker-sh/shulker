// The v2 probe starts the way a shulker process does on Charm v2 and exits: every package's init
// runs, colour is detected, and nothing asks the terminal anything.
package main

import (
	"fmt"
	"os"

	_ "charm.land/bubbles/v2/table"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/glamour/v2"
	_ "charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

func main() {
	w := colorprofile.NewWriter(os.Stderr, os.Environ())
	fmt.Fprintln(w, lipgloss.NewStyle().Bold(true).Render("started"))
}
