package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"encryptlock/lockkeeper/app"
	"encryptlock/lockkeeper/i18n"
	"encryptlock/lockkeeper/wallet"
	registrytypes "encryptlock/lockkeeper/x/registry/types"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
)

const flagLang = "lang"

// waitForBlock is how long a command waits for its transaction to be in a block.
const waitForBlock = 30 * time.Second

// walletCmd is `lockkeeper wallet`: one command per operation, every
// transaction signed with all the keys the account needs.
func walletCmd() *cobra.Command {
	help := i18n.New("") // help texts follow LOCKKEEPER_LANG
	cmd := &cobra.Command{
		Use:   "wallet",
		Short: help.T("wallet.short"),
		RunE:  client.ValidateCmd,
	}
	cmd.PersistentFlags().String(flagLang, "", help.T("flag.lang", strings.Join(i18n.Languages(), ", "), i18n.EnvVar))

	cmd.AddCommand(
		withTxFlags(&cobra.Command{Use: "create [name]", Short: help.T("create.short"), Args: cobra.ExactArgs(1), RunE: runCreate}),
		withTxFlags(&cobra.Command{Use: "recover [name]", Short: help.T("recover.short"), Args: cobra.ExactArgs(1), RunE: runRecover}),
		withTxFlags(&cobra.Command{Use: "register [name]", Short: help.T("register.short"), Args: cobra.ExactArgs(1), RunE: runRegister}),
		withTxFlags(&cobra.Command{Use: "balance [name-or-address]", Short: help.T("balance.short"), Args: cobra.ExactArgs(1), RunE: runBalance}),
		withTxFlags(&cobra.Command{Use: "send [from] [to] [LOCK]", Short: help.T("send.short"), Args: cobra.ExactArgs(3), RunE: runSend}),
		withTxFlags(&cobra.Command{Use: "pending [name-or-address]", Short: help.T("pending.short"), Args: cobra.ExactArgs(1), RunE: runPending}),
		withTxFlags(&cobra.Command{Use: "accept [name] [id]", Short: help.T("accept.short"), Args: cobra.ExactArgs(2), RunE: runAccept}),
		withTxFlags(&cobra.Command{Use: "cancel [name] [id]", Short: help.T("cancel.short"), Args: cobra.ExactArgs(2), RunE: runCancel}),
		withTxFlags(&cobra.Command{Use: "rewards [name]", Short: help.T("rewards.short"), Args: cobra.ExactArgs(1), RunE: runRewards}),
	)
	cmd.AddCommand(autoCollectCmd(help), computerHelperCmd(help))
	cmd.AddCommand(shieldCmds(help)...)
	cmd.AddCommand(recoveryCmds(help)...)
	cmd.AddCommand(vaultCmds(help)...)
	return cmd
}

func withTxFlags(cmd *cobra.Command) *cobra.Command {
	flags.AddTxFlagsToCmd(cmd)
	// On an error, show only the error, not the whole list of flags.
	cmd.SilenceUsage = true
	return cmd
}

func translator(cmd *cobra.Command) i18n.Translator {
	lang, _ := cmd.Flags().GetString(flagLang)
	return i18n.New(lang)
}

// clientFor returns the client context and translator of a wallet command.
func clientFor(cmd *cobra.Command) (client.Context, i18n.Translator, error) {
	clientCtx, err := client.GetClientTxContext(cmd)
	return clientCtx, translator(cmd), err
}

// address resolves a wallet name or an address, with a translated error.
func address(clientCtx client.Context, tr i18n.Translator, nameOrAddress string) (sdk.AccAddress, error) {
	addr, err := wallet.Address(clientCtx.Keyring, nameOrAddress)
	if err != nil {
		return nil, errors.New(tr.T("err.no_wallet", nameOrAddress))
	}
	return addr, nil
}

func runCreate(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	rec, words, err := wallet.Create(clientCtx.Keyring, args[0])
	if err != nil {
		return err
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return err
	}
	cmd.Println(tr.T("create.done", args[0], addr))
	cmd.Println()
	cmd.Println(tr.T("create.words_warning"))
	cmd.Println()
	cmd.Println(words)
	cmd.Println()
	cmd.Println(tr.T("create.next", args[0]))
	return nil
}

func runRecover(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	cmd.Println(tr.T("recover.prompt"))
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return err
	}
	rec, err := wallet.Recover(clientCtx.Keyring, args[0], line)
	if errors.Is(err, wallet.ErrWords) {
		return errors.New(tr.T("err.words"))
	}
	if errors.Is(err, keyring.ErrDuplicatedAddress) {
		return errors.New(tr.T("err.already_here"))
	}
	if err != nil {
		return err
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return err
	}
	cmd.Println(tr.T("recover.done", args[0], addr))
	return nil
}

func runRegister(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	res, err := registrytypes.NewQueryClient(clientCtx).Registered(cmd.Context(), &registrytypes.QueryRegisteredRequest{Address: addr.String()})
	if err != nil {
		return err
	}
	if res.Registered {
		cmd.Println(tr.T("register.already", args[0]))
		return nil
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &registrytypes.MsgRegister{Address: addr.String()}); err != nil {
		return err
	}
	cmd.Println(tr.T("register.done", args[0]))
	return nil
}

func runBalance(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	res, err := banktypes.NewQueryClient(clientCtx).Balance(cmd.Context(), &banktypes.QueryBalanceRequest{Address: addr.String(), Denom: app.BaseDenom})
	if err != nil {
		return err
	}
	cmd.Println(tr.T("balance.line", args[0], wallet.FormatLOCK(res.Balance.Amount.Uint64()), addr))
	return nil
}

func runSend(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	from, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	to, err := address(clientCtx, tr, args[1])
	if err != nil {
		return err
	}
	amount, err := wallet.ParseLOCK(args[2])
	if err != nil {
		return errors.New(tr.T("err.amount", args[2]))
	}
	reg, err := registrytypes.NewQueryClient(clientCtx).Registered(cmd.Context(), &registrytypes.QueryRegisteredRequest{Address: to.String()})
	if err != nil {
		return err
	}
	if !reg.Registered {
		return errors.New(tr.T("send.not_registered", args[1]))
	}

	res, err := broadcast(cmd, clientCtx, tr, args[0], &transferstypes.MsgSend{Sender: from.String(), Recipient: to.String(), Amount: amount})
	if err != nil {
		return err
	}
	ev := eventAttributes(res, "transfer_created")
	id, _ := strconv.ParseUint(ev["id"], 10, 64)
	releaseAt, _ := strconv.ParseInt(ev["release_at"], 10, 64)
	createdAt, _ := strconv.ParseInt(ev["created_at"], 10, 64)
	switch {
	case ev["awaiting_approval"] == "true":
		cmd.Println(tr.T("send.done_awaiting", id, wallet.FormatLOCK(amount), args[0], args[1]))
	case releaseAt > createdAt:
		cmd.Println(tr.T("send.done_vault", id, wallet.FormatLOCK(amount), args[0], args[1], when(releaseAt)))
	default:
		cmd.Println(tr.T("send.done", id, wallet.FormatLOCK(amount), args[0], args[1]))
	}
	cmd.Println(tr.T("send.fee", args[0], lockAttr(ev["fee"]), lockAttr(ev["fee_dev_fund"]), lockAttr(ev["fee_lockkeepers"])))
	if ev["awaiting_approval"] == "true" {
		cmd.Println(tr.T("send.awaiting", args[0]))
	}
	return nil
}

func runPending(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	res, err := transferstypes.NewQueryClient(clientCtx).Pending(cmd.Context(), &transferstypes.QueryPendingRequest{Address: addr.String()})
	if err != nil {
		return err
	}
	expires := func(t *transferstypes.Transfer) string {
		return time.Unix(t.ExpiresAt, 0).Local().Format("2006-01-02 15:04:05")
	}
	cmd.Println(tr.T("pending.sent_header", args[0]))
	for _, t := range res.Sent {
		cmd.Println(tr.T("pending.sent_line", t.Id, wallet.FormatLOCK(t.Amount), t.Recipient, expires(t)) + awaitingText(tr, t))
	}
	if len(res.Sent) == 0 {
		cmd.Println(tr.T("pending.none"))
	}
	cmd.Println(tr.T("pending.received_header", args[0]))
	for _, t := range res.Received {
		cmd.Println(tr.T("pending.received_line", t.Id, wallet.FormatLOCK(t.Amount), t.Sender, expires(t)) + awaitingText(tr, t))
	}
	if len(res.Received) == 0 {
		cmd.Println(tr.T("pending.none"))
	}
	return nil
}

func runAccept(cmd *cobra.Command, args []string) error {
	return runTransferAction(cmd, args, "accept.done", func(addr string, id uint64) sdk.Msg {
		return &transferstypes.MsgAccept{Recipient: addr, Id: id}
	})
}

func runCancel(cmd *cobra.Command, args []string) error {
	return runTransferAction(cmd, args, "cancel.done", func(addr string, id uint64) sdk.Msg {
		return &transferstypes.MsgCancel{Sender: addr, Id: id}
	})
}

func runTransferAction(cmd *cobra.Command, args []string, doneKey string, msg func(addr string, id uint64) sdk.Msg) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(args[1], "#"), 10, 64)
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], msg(addr.String(), id)); err != nil {
		return err
	}
	cmd.Println(tr.T(doneKey, id, args[0]))
	return nil
}

func runRewards(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	valAddr := sdk.ValAddress(addr)
	if _, err := stakingtypes.NewQueryClient(clientCtx).Validator(cmd.Context(), &stakingtypes.QueryValidatorRequest{ValidatorAddr: valAddr.String()}); err != nil {
		return errors.New(tr.T("err.not_validator", args[0]))
	}
	// The reward on its own guarantee, then its LockKeeper commission.
	msg := &distrtypes.MsgWithdrawDelegatorReward{DelegatorAddress: addr.String(), ValidatorAddress: valAddr.String()}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], msg); err != nil {
		return err
	}
	commission := &distrtypes.MsgWithdrawValidatorCommission{ValidatorAddress: valAddr.String()}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], commission); err != nil {
		return err
	}
	cmd.Println(tr.T("rewards.done", args[0]))
	return nil
}

// broadcast builds msg, signs it with every key the account needs, sends it
// and waits until it is in a block.
func broadcast(cmd *cobra.Command, clientCtx client.Context, tr i18n.Translator, name string, msg sdk.Msg) (*sdk.TxResponse, error) {
	rec, err := clientCtx.Keyring.Key(name)
	if err != nil {
		return nil, errors.New(tr.T("err.no_wallet", name))
	}
	pubKey, err := rec.GetPubKey()
	if err != nil {
		return nil, err
	}
	pk, ok := pubKey.(*multisig.LegacyAminoPubKey)
	if !ok {
		return nil, errors.New(tr.T("err.no_wallet", name))
	}
	addr := sdk.AccAddress(pk.Address())

	// A brand-new account (registering) is not known yet: number 0, sequence 0.
	accNum, seq, err := clientCtx.AccountRetriever.GetAccountNumberSequence(clientCtx, addr)
	if err != nil {
		if _, isRegister := msg.(*registrytypes.MsgRegister); !isRegister {
			return nil, err
		}
		accNum, seq = 0, 0
	}

	txBuilder := clientCtx.TxConfig.NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return nil, err
	}
	// Gas covers the large post-quantum signatures; it costs nothing (fees
	// are charged by EncryptLock's own rules).
	txBuilder.SetGasLimit(uint64(300_000 + 250_000*wallet.LeafSignatures(pk)))

	if err := wallet.SignTx(cmd.Context(), clientCtx.TxConfig, clientCtx.Keyring, clientCtx.ChainID, accNum, seq, pk, txBuilder); err != nil {
		var notEnough wallet.NotEnoughKeysError
		if errors.As(err, &notEnough) {
			return nil, errors.New(tr.T("err.not_enough_keys", name, notEnough.Have, notEnough.Need))
		}
		return nil, err
	}

	txBytes, err := clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}
	res, err := clientCtx.WithBroadcastMode(flags.BroadcastSync).BroadcastTx(txBytes)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, chainError(tr, res)
	}
	return waitForTx(cmd.Context(), clientCtx, tr, res.TxHash)
}

func waitForTx(ctx context.Context, clientCtx client.Context, tr i18n.Translator, hash string) (*sdk.TxResponse, error) {
	deadline := time.Now().Add(waitForBlock)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		res, err := authtx.QueryTx(clientCtx, hash)
		if err != nil {
			continue // not in a block yet
		}
		if res.Code != 0 {
			return nil, chainError(tr, res)
		}
		return res, nil
	}
	return nil, errors.New(tr.T("err.timeout", int(waitForBlock.Seconds()), hash))
}

// eventAttributes returns the attributes of the first event of a type.
func eventAttributes(res *sdk.TxResponse, eventType string) map[string]string {
	out := map[string]string{}
	for _, ev := range res.Events {
		if ev.Type != eventType {
			continue
		}
		for _, attr := range ev.Attributes {
			out[attr.Key] = attr.Value
		}
		break
	}
	return out
}

func lockAttr(units string) string {
	n, _ := strconv.ParseUint(units, 10, 64)
	return wallet.FormatLOCK(n)
}

func awaitingText(tr i18n.Translator, t *transferstypes.Transfer) string {
	if t.AwaitingApproval {
		return tr.T("pending.awaiting")
	}
	if t.ReleaseAt > time.Now().Unix() {
		return tr.T("pending.release", when(t.ReleaseAt))
	}
	return ""
}

// chainError explains, in the chosen language, why the network rejected a
// transaction (EncryptLock's own errors and the most common Cosmos ones).
func chainError(tr i18n.Translator, res *sdk.TxResponse) error {
	key := fmt.Sprintf("chain.%s.%d", res.Codespace, res.Code)
	if text := tr.T(key); text != key {
		return errors.New(text)
	}
	return errors.New(tr.T("err.rejected", res.RawLog))
}
