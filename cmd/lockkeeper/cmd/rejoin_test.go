package cmd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The network back after a long stop: a LockKeeper alone for 3 minutes
// starts again, then not more than every 5 minutes, and never while it has
// a neighbour or does not answer.
func TestAloneWatch(t *testing.T) {
	var w aloneWatch
	t0 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	if w.due(at(0), 3, true) || w.due(at(2), 0, true) {
		t.Fatal("restart before 3 minutes alone")
	}
	if w.due(at(4), 0, false) {
		t.Fatal("restart while the LockKeeper does not answer")
	}
	if !w.due(at(6), 0, true) {
		t.Fatal("no restart after 4 minutes alone")
	}
	// Still alone: the network is really off.
	if w.due(at(8), 0, true) || w.due(at(10), 0, true) {
		t.Fatal("restart again within 5 minutes")
	}
	if !w.due(at(12), 0, true) {
		t.Fatal("no second restart after 6 minutes alone again")
	}
	// A neighbour came back: everything starts over.
	if w.due(at(14), 1, true) || w.due(at(30), 0, true) || w.due(at(32), 0, true) {
		t.Fatal("restart right after a neighbour came back")
	}
	if !w.due(at(34), 0, true) {
		t.Fatal("no restart after being alone again")
	}
}

func TestNeighbours(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/net_info" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":-1,"result":{"listening":true,"n_peers":"2","peers":[]}}`))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "[rpc]\nladdr = \"tcp://127.0.0.1:" + port + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, ok := neighbours(home); !ok || n != 2 {
		t.Fatalf("neighbours = %d, %v", n, ok)
	}
	srv.Close()
	if _, ok := neighbours(home); ok {
		t.Fatal("a LockKeeper that does not answer counted as alone")
	}
}

// The founder's computer (no door-p2p.txt) is never restarted.
func TestRejoinLeavesTheDoorAlone(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "running.json"), []byte(`{"node":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	alone = aloneWatch{since: time.Now().Add(-time.Hour)}
	rejoinIfAlone(home)
	if !alone.since.Before(time.Now().Add(-30 * time.Minute)) {
		t.Fatal("the door's own LockKeeper was looked at")
	}
}

// Stuck with its neighbour: started again only when the network's oldest
// block is past this computer's newest on two looks in a row, never while
// it joins from the copy (0) or keeps up.
func TestStuckWatch(t *testing.T) {
	var w stuckWatch
	if w.due(1000, 900) || w.due(1000, 1001) {
		t.Fatal("restart while the network still keeps its next block")
	}
	if w.due(1000, 1500) {
		t.Fatal("restart on the first look")
	}
	if !w.due(1000, 1510) {
		t.Fatal("no restart on the second look")
	}
	if w.due(0, 1600) || w.due(0, 1700) {
		t.Fatal("restart while joining from the copy")
	}
	if w.due(1000, 1500) || w.due(1700, 1600) || w.due(1000, 1500) {
		t.Fatal("looks not counted again after catching up")
	}
}
