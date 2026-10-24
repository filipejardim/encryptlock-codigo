package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNeighbourPort(t *testing.T) {
	cfg := "x = 1\npersistent_peers = \"abc123@127.0.0.1:28158\"\n"
	m := neighbourPort.FindSubmatch([]byte(cfg))
	if m == nil || string(m[1]) != "28158" {
		t.Fatalf("port not found: %q", m)
	}
	if neighbourPort.Match([]byte("persistent_peers = \"abc@10.0.0.2:26656\"\n")) {
		t.Fatal("a neighbour that is not the local door end must not match")
	}
}

// A computer that did not join from another home, or whose LockKeeper is not
// running, is left alone (no files written, nothing started).
func TestFollowDoorLeavesOthersAlone(t *testing.T) {
	home := t.TempDir()
	followDoor(home)
	if _, err := os.Stat(filepath.Join(home, doorP2PTxt)); err == nil {
		t.Fatal("door file written without joining from another home")
	}
	if err := os.WriteFile(filepath.Join(home, doorP2PTxt), []byte("old.trycloudflare.com"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "running.json"), []byte(`{"node":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	followDoor(home)
	b, _ := os.ReadFile(filepath.Join(home, doorP2PTxt))
	if string(b) != "old.trycloudflare.com" {
		t.Fatalf("door changed while the LockKeeper is off: %s", b)
	}
}
