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
	restartNode(home, pidFile, pids)
}

// restartNode stops this computer's LockKeeper and starts it again; the new
// start (prepareStateSync) joins from the copy of the state if it missed
// too much.
func restartNode(home, pidFile string, pids map[string]int) {
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

// A LockKeeper can also be stuck WITH its neighbour: off the internet (or
// asleep) for longer than the network keeps whole blocks (about 40
// minutes) while it kept running. Back online it waits for blocks that
// nobody has any more, shows "catching up" for ever and its "I am on"
// never reaches the network (24 Oct 2026: a friend's computer, hours, even
// after reinstalling the app). Only a new start can join from the copy of
// the state, so when the network's oldest block is past this computer's
// newest on two looks in a row, it starts again.
type stuckWatch struct{ looks int }

// due records one look (local: this computer's newest block, 0 while it
// joins from the copy; earliest: the network's oldest kept block) and
// reports whether to start it again now.
func (w *stuckWatch) due(local, earliest int64) bool {
	if local <= 0 || earliest <= 0 || local+1 >= earliest {
		w.looks = 0
		return false
	}
	w.looks++
	if w.looks < 2 {
		return false
	}
	w.looks = 0
	return true
}

var stuck stuckWatch

// rejoinIfStuck starts this computer's LockKeeper again when the network no
// longer keeps the blocks it needs. Same computers as rejoinIfAlone.
func rejoinIfStuck(home string) {
	if !fileExists(filepath.Join(home, doorP2PTxt)) {
		return
	}
	pidFile := filepath.Join(home, "running.json")
	raw, err := os.ReadFile(pidFile)
	pids := map[string]int{}
	if err != nil || json.Unmarshal(raw, &pids) != nil || !processAlive(pids["node"]) {
		return
	}
	local, ok := localHeight(home)
	if !ok {
		return
	}
	rpc := doorRPCFromSite()
	if rpc == "" {
		return
	}
	_, earliest, err := doorHeights(rpc)
	if err != nil || !stuck.due(local, earliest) {
		return
	}
	restartNode(home, pidFile, pids)
}

// localHeight asks this computer's LockKeeper for its newest block (ok =
// false when it does not answer).
func localHeight(home string) (int64, bool) {
	port, ok := localRPCPort(home)
	if !ok {
		return 0, false
	}
	var status struct {
		Result struct {
			SyncInfo struct {
				Latest string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	c := http.Client{Timeout: netInfoTimeout}
	res, err := c.Get(fmt.Sprintf("http://127.0.0.1:%s/status", port))
	if err != nil {
		return 0, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&status) != nil {
		return 0, false
	}
	h, err := strconv.ParseInt(status.Result.SyncInfo.Latest, 10, 64)
	return h, err == nil
}

// neighbours asks this computer's LockKeeper how many neighbours it has
// (ok = false when it does not answer).
func neighbours(home string) (int, bool) {
	port, ok := localRPCPort(home)
	if !ok {
		return 0, false
	}
	c := http.Client{Timeout: netInfoTimeout}
	res, err := c.Get(fmt.Sprintf("http://127.0.0.1:%s/net_info", port))
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

// localRPCPort is the port of this computer's LockKeeper RPC (config.toml).
func localRPCPort(home string) (string, bool) {
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	if err != nil {
		return "", false
	}
	m := rpcLaddr.FindSubmatch(cfg)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}
