package loader

import (
	"context"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestFakeWithAServerHookKeepsTheRowsPlacement(t *testing.T) {
	lk := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "neoforge", Version: "26.2.0.87"}}
	called := 0
	row := Fake{Name: "neoforge", EnsureServer: func(_ context.Context, _ *Remote, lk *lock.Lock) (ServerResult, error) {
		called++
		lk.Loader.Server = &lock.ServerJar{Sha512: "abc"}
		return ServerResult{ChangedLock: true}, nil
	}}.Row()
	res, err := row.EnsureServer(context.Background(), &Remote{}, lk)
	if err != nil || called != 1 || !res.ChangedLock || lk.Loader.Server == nil {
		t.Fatalf("hook: %v, called %d, %+v, %+v", err, called, res, lk.Loader.Server)
	}
	if row.InstallServerFlag != neoforge.InstallServerFlag || !slices.Equal(row.LaunchArgs(lk), neoforge.LaunchArgs(lk)) || row.VanillaServerPath("26.2") != neoforge.VanillaServerPath("26.2") {
		t.Fatalf("a fake with a server keeps the real row's placement: %q %v %q", row.InstallServerFlag, row.LaunchArgs(lk), row.VanillaServerPath("26.2"))
	}

	bare := Fake{Name: "neoforge"}.Row()
	lk.Loader.Server = nil
	if res, err := bare.EnsureServer(context.Background(), &Remote{}, lk); err != nil || res != (ServerResult{}) || lk.Loader.Server != nil {
		t.Fatalf("a fake without a hook has no server: %v %+v", err, res)
	}
	if bare.InstallServerFlag != "" || !slices.Equal(bare.LaunchArgs(lk), []string{"-jar", VanillaServerFile}) || bare.VanillaServerPath("26.2") != VanillaServerFile {
		t.Fatalf("a fake without a server has vanilla placement: %q %v %q", bare.InstallServerFlag, bare.LaunchArgs(lk), bare.VanillaServerPath("26.2"))
	}
}
