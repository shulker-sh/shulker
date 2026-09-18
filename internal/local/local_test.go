package local

import (
	"testing"
)

func TestSyncDirsAreKeyedBySide(t *testing.T) {
	f := &File{}
	if !f.RecordSyncDir("client", "/a") || f.RecordSyncDir("client", "/a") || !f.RecordSyncDir("client", "/b") || !f.RecordSyncDir("server", "/srv") {
		t.Fatalf("record: %+v", f.SyncDirs)
	}
	if len(f.SyncDirs["client"]) != 2 || len(f.SyncDirs["server"]) != 1 {
		t.Fatalf("sync dirs: %+v", f.SyncDirs)
	}
	if f.RemoveSyncDir("client", "/missing") || !f.RemoveSyncDir("client", "/a") || len(f.SyncDirs["client"]) != 1 {
		t.Fatalf("remove one: %+v", f.SyncDirs)
	}
	if !f.RemoveSyncDir("server", "/srv") {
		t.Fatal("remove the last server dir")
	}
	if _, ok := f.SyncDirs["server"]; ok {
		t.Fatalf("the side's key should go with its last entry: %+v", f.SyncDirs)
	}
}
