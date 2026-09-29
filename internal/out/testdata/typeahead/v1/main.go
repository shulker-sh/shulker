// The v1 probe starts the way a shulker process does on Charm v1 and exits. Bubble Tea's init
// asks the terminal for its background, which is what loses input typed ahead.
package main

import (
	"fmt"
	"os"

	_ "github.com/charmbracelet/bubbles/table"
	_ "github.com/charmbracelet/bubbletea"
	_ "github.com/charmbracelet/glamour"
	_ "github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Bold(true).Render("started"))
}
