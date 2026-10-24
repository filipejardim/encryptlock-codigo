package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
)

// pcAutostartCmd is what Windows runs when it starts, if the owner chose
// "Switch on by itself when Windows starts" in the EncryptLock app: it starts
// this computer's blockchain LockKeeper and its "I am on" signal - the same
// two programs the app starts - without opening the app or any window, and
// exits. Nothing happens if they are already running.
func pcAutostartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pc-autostart",
		Short: "Start this computer's blockchain LockKeeper in the background (used when Windows starts)",
		Args:  cobra.NoArgs,
		RunE:  runPCAutostart,
	}
	cmd.Flags().String(flags.FlagHome, "", "the computer's LockKeeper folder")
	cmd.SilenceUsage = true
	return cmd
}

var rpcLaddr = regexp.MustCompile(`(?m)^laddr = "tcp://127\.0\.0\.1:(\d+)"`)

func runPCAutostart(cmd *cobra.Command, _ []string) error {
	home, _ := cmd.Flags().GetString(flags.FlagHome)
	if home == "" {
		return errors.New("--home is required")
	}
	if _, err := os.Stat(filepath.Join(home, "lockkeeper-ready")); err != nil {
		return nil // never made a LockKeeper on this computer
	}
	pidFile := filepath.Join(home, "running.json")
	if b, err := os.ReadFile(pidFile); err == nil {
		var pids map[string]int
		if json.Unmarshal(b, &pids) == nil && processAlive(pids["node"]) && processAlive(pids["heartbeat"]) {
			return nil
		}
	}
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	if err != nil {
		return err
	}
	m := rpcLaddr.FindSubmatch(cfg)
	if m == nil {
		return errors.New("rpc address not found in config.toml")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	pids := map[string]int{}
	// Joined from another home (the app wrote door-p2p.txt): open this
	// computer's end of the direct connection through Cloudflare first, as
	// the app does.
	if bridge := doorBridge(self, home); bridge != nil {
		background(bridge)
		if err := bridge.Start(); err == nil {
			pids["door"] = bridge.Process.Pid
		}
	}
	node := exec.Command(self, "start", "--home", home)
	beat := exec.Command(self, "pc-heartbeat", filepath.Join(home, "helper-24-words.txt"),
		"--node", "tcp://127.0.0.1:"+string(m[1]), "--home", home)
	for _, c := range []*exec.Cmd{node, beat} {
		background(c)
		if err := c.Start(); err != nil {
			return err
		}
	}
	pids["node"], pids["heartbeat"] = node.Process.Pid, beat.Process.Pid
	b, _ := json.Marshal(pids)
	if err := os.WriteFile(pidFile, b, 0o600); err != nil {
		return err
	}
	startDoor(home)
	return nil
}

// Same values as the app (PcLockKeeper.doorPort, NetworkDoor).
const (
	doorPort   = "27058"
	doorSite   = "https://encryptlock-rede.pages.dev/rede.json"
	doorP2PTxt = "door-p2p.txt"
)

// doorHost accepts only plain host names (it goes into a command): labels of
// letters, digits and dashes that never start or end with a dash.
var doorHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// doorBridge returns "cloudflared access tcp" to the network's door when this
// LockKeeper joined from another home, with the newest door address the
// website publishes (it changes when the founder's computer restarts); nil
// otherwise.
func doorBridge(self, home string) *exec.Cmd {
	file := filepath.Join(home, doorP2PTxt)
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	host := strings.TrimSpace(string(b))
	if now := doorFromSite(); now != "" && now != host {
		host = now
		_ = os.WriteFile(file, []byte(host), 0o600)
	}
	if !doorHost.MatchString(host) {
		return nil
	}
	cf := filepath.Join(filepath.Dir(self), "cloudflared.exe")
	if _, err := os.Stat(cf); err != nil {
		return nil
	}
	return exec.Command(cf, "access", "tcp", "--hostname", host, "--url", "127.0.0.1:"+doorPort)
}

// doorFromSite reads the p2p door host from the website ("" if unknown).
func doorFromSite() string {
	c := &http.Client{Timeout: 15 * time.Second}
	res, err := c.Get(doorSite)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var j struct {
		P2P string `json:"p2p"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&j) != nil || !doorHost.MatchString(j.P2P) {
		return ""
	}
	return j.P2P
}

// startDoor opens the network's door to the outside (phones away from home,
// the website) when this computer is the one that does it: the founder's
// computer has the path of scriptsede-publica.ps1 in porta-publica.txt.
// Anyone else's computer has no such file and nothing happens.
func startDoor(home string) {
	b, err := os.ReadFile(filepath.Join(home, "porta-publica.txt"))
	if err != nil {
		return
	}
	door := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", strings.TrimSpace(string(b)))
	hidden(door)
	_ = door.Start()
}
