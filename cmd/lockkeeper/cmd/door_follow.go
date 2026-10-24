package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// How often a LockKeeper that joined from another home looks at the door's
// address on the website.
const doorFollowEvery = 2 * time.Minute

// neighbourPort is the local port of the door's end in config.toml
// (persistent_peers = "<id>@127.0.0.1:<port>").
var neighbourPort = regexp.MustCompile(`(?m)^persistent_peers = ".*@127\.0\.0\.1:(\d+)`)

// followDoor keeps a LockKeeper that joined from another home connected to
// the network's door. The door's address changes every time the founder's
// computer restarts; the direct connection ("cloudflared access tcp") was
// opened with the old one and would stay pointed at nothing until this
// computer restarts too. So: when the website shows a new address, or the
// connection program has stopped, open it again with the newest address on
// the same local port. Nothing happens on a computer that did not join from
// another home (no door-p2p.txt) or was not started by the app or by Windows
// (no running.json).
func followDoor(home string) {
	file := filepath.Join(home, doorP2PTxt)
	b, err := os.ReadFile(file)
	if err != nil {
		return
	}
	host := strings.TrimSpace(string(b))
	pidFile := filepath.Join(home, "running.json")
	raw, err := os.ReadFile(pidFile)
	pids := map[string]int{}
	if err != nil || json.Unmarshal(raw, &pids) != nil || !processAlive(pids["node"]) {
		return
	}
	now := doorFromSite()
	alive := processIs(pids["door"], "cloudflared")
	if alive && (now == "" || now == host) {
		return
	}
	if now != "" {
		host = now
	}
	if !doorHost.MatchString(host) {
		return
	}
	port := doorPort
	if cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml")); err == nil {
		if m := neighbourPort.FindSubmatch(cfg); m != nil {
			port = string(m[1])
		}
	}
	cf := cloudflaredFor(home)
	if cf == "" {
		return
	}
	if alive {
		if p, err := os.FindProcess(pids["door"]); err == nil {
			_ = p.Kill()
			_, _ = p.Wait()
		}
		time.Sleep(time.Second) // the local port is free again
	}
	bridge := exec.Command(cf, "access", "tcp", "--hostname", host, "--url", "127.0.0.1:"+port)
	background(bridge)
	if bridge.Start() != nil {
		return
	}
	_ = os.WriteFile(file, []byte(host), 0o600)
	pids["door"] = bridge.Process.Pid
	if out, err := json.Marshal(pids); err == nil {
		_ = os.WriteFile(pidFile, out, 0o600)
	}
}

// cloudflaredFor is the connection program: the one next to this lockkeeper
// (the app's), or one in the app's own folder.
func cloudflaredFor(home string) string {
	if self, err := os.Executable(); err == nil {
		if cf := filepath.Join(filepath.Dir(self), "cloudflared.exe"); fileExists(cf) {
			return cf
		}
	}
	for _, dir := range []string{filepath.Dir(home), filepath.Join(os.Getenv("LOCALAPPDATA"), "EncryptLock")} {
		for _, name := range []string{"cloudflared.exe", "cloudflared-2026.10.0.exe"} {
			if cf := filepath.Join(dir, name); fileExists(cf) {
				return cf
			}
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
