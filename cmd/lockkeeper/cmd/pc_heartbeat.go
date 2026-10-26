package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	core "encryptlock/lockkeeper/mobilecore"
	"encryptlock/lockkeeper/protocol"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
)

// pcHeartbeatCmd runs next to the LockKeeper of a computer made a blockchain
// LockKeeper by the EncryptLock app. It uses the helper account whose 24
// words are in the file (no coins; the owner's password is never needed):
//  1. the first time, the helper registers itself (free);
//  2. if the app left the network's chip ticket (chip-ticket.json in --home),
//     it proves this computer's chip (free; without it the network does not
//     accept "I am on");
//  3. until a LockKeeper account uses this computer, it publishes the
//     computer's block-signing key (from --home), so a phone that scans the
//     computer's QR code can make it a LockKeeper;
//  4. then, every 5 minutes, it says "I am on" through this computer's
//     LockKeeper (--node).
func pcHeartbeatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pc-heartbeat [helper-words-file]",
		Short: "Say \"I am on\" every 5 minutes for this computer's blockchain LockKeeper (used by the EncryptLock app)",
		Args:  cobra.ExactArgs(1),
		RunE:  runPCHeartbeat,
	}
	flags.AddTxFlagsToCmd(cmd)
	cmd.SilenceUsage = true
	return cmd
}

// pcStep is what one round did.
type pcStep int

const (
	pcRegistered pcStep = iota // the helper registered itself
	pcChip                     // the chip ticket was sent
	pcAnnounced                // the key was published; waiting for a phone
	pcWaiting                  // the key is published; still waiting
	pcBeat                     // "I am on" sent
)

func runPCHeartbeat(cmd *cobra.Command, args []string) error {
	if home, _ := cmd.Flags().GetString(flags.FlagHome); home != "" {
		go func() {
			for {
				select {
				case <-cmd.Context().Done():
					return
				case <-time.After(doorFollowEvery):
					followDoor(home)
					rejoinIfAlone(home)
					rejoinIfStuck(home)
				}
			}
		}()
	}
	for {
		wait := protocol.HeartbeatEvery
		step, err := pcRound(cmd, args[0])
		switch {
		case err != nil:
			cmd.PrintErrln("heartbeat:", err)
			wait = time.Minute // the LockKeeper may still be starting
		case step == pcRegistered:
			cmd.Println("helper registered", time.Now().Format(time.TimeOnly))
			wait = 10 * time.Second
		case step == pcChip:
			cmd.Println("security chip proven", time.Now().Format(time.TimeOnly))
			wait = 10 * time.Second
		case step == pcAnnounced || step == pcWaiting:
			if step == pcAnnounced {
				cmd.Println("key published, waiting for a phone to link this computer", time.Now().Format(time.TimeOnly))
			}
			wait = 10 * time.Second // start soon after the phone links it
		default:
			cmd.Println("heartbeat sent", time.Now().Format(time.TimeOnly))
		}
		select {
		case <-cmd.Context().Done():
			return nil
		case <-time.After(wait):
		}
	}
}

func pcRound(cmd *cobra.Command, wordsFile string) (pcStep, error) {
	raw, err := os.ReadFile(wordsFile)
	if err != nil {
		return 0, err
	}
	words := strings.TrimSpace(string(raw))
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return 0, err
	}
	chainID := clientCtx.ChainID
	if chainID == "" {
		st, err := clientCtx.Client.Status(cmd.Context())
		if err != nil {
			return 0, err
		}
		chainID = st.NodeInfo.Network
	}
	addrStr, err := core.Address(words)
	if err != nil {
		return 0, err
	}
	addr, err := sdk.AccAddressFromBech32(addrStr)
	if err != nil {
		return 0, err
	}

	var tx string
	step := pcBeat
	accNum, seq, err := clientCtx.AccountRetriever.GetAccountNumberSequence(clientCtx, addr)
	if err != nil {
		// First time: the helper is not known yet; it registers (free).
		step = pcRegistered
		tx, err = core.SignRegister(words, chainID, "0", 0)
	} else {
		info, qerr := rotationtypes.NewQueryClient(clientCtx).Helper(cmd.Context(), &rotationtypes.QueryHelperRequest{Helper: addrStr})
		if qerr != nil {
			return 0, qerr
		}
		ticket := chipTicket(clientCtx.HomeDir, addrStr)
		switch {
		case info.Chip == "" && ticket != nil:
			step = pcChip
			tx, err = core.SignProveChip(words, chainID, strconv.FormatUint(accNum, 10), int64(seq), ticket.Code, base64.StdEncoding.EncodeToString(ticket.Ticket))
		case info.Keeper != "":
			tx, err = core.SignHeartbeat(words, chainID, strconv.FormatUint(accNum, 10), int64(seq))
		case len(info.PubKey) > 0:
			return pcWaiting, nil
		default:
			key, kerr := consPubKey(clientCtx.HomeDir)
			if kerr != nil {
				return 0, kerr
			}
			step = pcAnnounced
			tx, err = core.SignAnnounceKey(words, chainID, strconv.FormatUint(accNum, 10), int64(seq), key)
		}
	}
	if err != nil {
		return 0, err
	}
	bz, err := base64.StdEncoding.DecodeString(tx)
	if err != nil {
		return 0, err
	}
	res, err := clientCtx.WithBroadcastMode(flags.BroadcastSync).BroadcastTx(bz)
	if err != nil {
		return 0, err
	}
	if res.Code != 0 {
		if step == pcChip {
			// Not accepted (another network, or the chip serves another
			// account): keep it aside so the other steps go on.
			// It is tried again after chipRetryAfter (see chipTicket).
			p := filepath.Join(clientCtx.HomeDir, chipTicketFile)
			if os.Rename(p, p+".refused") == nil {
				_ = os.Chtimes(p+".refused", time.Now(), time.Now())
			}
		}
		return 0, errors.New(res.RawLog)
	}
	return step, nil
}

// chipTicketFile is where the app leaves the network's chip ticket.
const chipTicketFile = "chip-ticket.json"

type chipTicketJSON struct {
	Account string `json:"account"`
	Code    string `json:"code"`
	Ticket  []byte `json:"ticket"`
}

// chipRetryAfter: a refused chip ticket is tried again after this long (it may
// be accepted later, e.g. from protocol.ChipMovesFrom when the computer served
// another account before), so a computer never stays stuck without its chip.
const chipRetryAfter = time.Hour

// chipTicket reads the ticket for account in home (nil if none).
func chipTicket(home, account string) *chipTicketJSON {
	p := filepath.Join(home, chipTicketFile)
	if st, err := os.Stat(p + ".refused"); err == nil && time.Since(st.ModTime()) >= chipRetryAfter {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			_ = os.Rename(p+".refused", p)
		}
	}
	bz, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var t chipTicketJSON
	if json.Unmarshal(bz, &t) != nil || t.Account != account || t.Code == "" || len(t.Ticket) == 0 {
		return nil
	}
	return &t
}

// consPubKey reads the base64 block-signing public key of the LockKeeper at
// home.
func consPubKey(home string) (string, error) {
	bz, err := os.ReadFile(filepath.Join(home, "config", "priv_validator_key.json"))
	if err != nil {
		return "", err
	}
	var key struct {
		PubKey struct {
			Value string `json:"value"`
		} `json:"pub_key"`
	}
	if err := json.Unmarshal(bz, &key); err != nil {
		return "", err
	}
	return key.PubKey.Value, nil
}
