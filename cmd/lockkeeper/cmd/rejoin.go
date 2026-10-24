package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// A LockKeeper that joined from another home talks to the network through
// one neighbour only: the door. When the network stops for a while (the
// founder's computer off or restarted), CometBFT tries the door again 20
// times in 100 seconds, then waits 3, 9, 27 … up to 16 hours between tries,
// and finally gives up: on 21 Oct 2026, after a 45-minute stop, a friend's
// computer stayed out for hours until it was restarted by hand. So the
// LockKeeper starts its own program again when it has had no neighbour for
// a few minutes: the new start tries at once. A computer with no neighbour
// signs nothing and writes nothing, so stopping it there loses nothing.
const (
	aloneRestartAfter  = 3 * time.Minute  // no neighbour for this long: start again
	minBetweenRestarts = 5 * time.Minute  // while the network is really off, not more often
	netInfoTimeout     = 10 * time.Second // the LockKeeper's own RPC, on this computer
)

// aloneWatch decides when a LockKeeper with no neighbour starts again.
type aloneWatch struct {
	since     time.Time // first look with no neighbour (zero: has one)
	restarted time.Time
}

// due records one look (peers = neighbours; ok = the LockKeeper answered)
// and reports whether to start it again now.
func (w *aloneWatch) due(now time.Time, peers int, ok bool) bool {
	if !ok {
		return false // starting, or switched off: nothing to judge
	}
	if peers > 0 {
		w.since = time.Time{}
		return false
	}
	if w.since.IsZero() {
		w.since = now
		return false
	}
	if now.Sub(w.since) < aloneRestartAfter || now.Sub(w.restarted) < minBetweenRestarts {
		return false
	}
	w.restarted, w.since = now, time.Time{}
	return true
}

var alone aloneWatch

// rejoinIfAlone starts this computer's LockKeeper again when it has had no
// neighbour for a while. Only on a computer that joined from another home
// (door-p2p.txt) and was started by the app or by Windows (running.json):
// the founder's computer, which is the door, is never touched.
func rejoinIfAlone(home string) {
	if !fileExists(filepath.Join(home, doorP2PTxt)) {
		return
	}
	pidFile := filepath.Join(home, "running.json")
	raw, err := os.ReadFile(pidFile)
	pids := map[string]int{}
	if err != nil || json.Unmarshal(raw, &pids) != nil || !processAlive(pids["node"]) {
		return
	}
	peers, ok := neighbours(home)
	if !alone.due(time.Now(), peers, ok) {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	if p, err := os.FindProcess(pids["node"]); err == nil {
		_ = p.Kill()
		_, _ = p.Wait()
	}
	time.Sleep(2 * time.Second) // its files and ports are free again
	node := exec.Command(self, "start", "--home", home)
	background(node)
	if node.Start() != nil {
		return
	}
	pids["node"] = node.Process.Pid
	if out, err := json.Marshal(pids); err == nil {
		_ = os.WriteFile(pidFile, out, 0o600)
	}
}

// neighbours asks this computer's LockKeeper how many neighbours it has
// (ok = false when it does not answer).
func neighbours(home string) (int, bool) {
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	if err != nil {
		return 0, false
	}
	m := rpcLaddr.FindSubmatch(cfg)
	if m == nil {
		return 0, false
	}
	c := http.Client{Timeout: netInfoTimeout}
	res, err := c.Get(fmt.Sprintf("http://127.0.0.1:%s/net_info", m[1]))
	if err != nil {
		return 0, false
	}
	defer res.Body.Close()
	var info struct {
		Result struct {
			NPeers string `json:"n_peers"`
		} `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&info) != nil {
		return 0, false
	}
	n, err := strconv.Atoi(info.Result.NPeers)
	return n, err == nil
}
