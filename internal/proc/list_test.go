package proc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListHoldsThisProcess(t *testing.T) {
	processes, err := List()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(self)
	for _, p := range processes {
		if strings.EqualFold(filepath.Base(p.Exe), name) {
			return
		}
	}
	t.Fatalf("%s isn't among the %d processes listed", name, len(processes))
}
