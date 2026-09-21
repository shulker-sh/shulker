package saves

import (
	"os"
	"testing"

	"shulker.sh/shulker/internal/saves/savestest"
)

func TestMain(m *testing.M) {
	if !savestest.Main() {
		return
	}
	os.Exit(m.Run())
}
