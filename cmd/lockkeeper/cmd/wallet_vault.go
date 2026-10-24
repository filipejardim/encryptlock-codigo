package cmd

import (
	"errors"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	"encryptlock/lockkeeper/hybrid"
	"encryptlock/lockkeeper/i18n"
	"encryptlock/lockkeeper/wallet"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
	vaulttypes "encryptlock/lockkeeper/x/vault/types"
)

// vaultCmds are the Sovereign Vault commands of `lockkeeper wallet`
// (white paper, section 7.4).
func vaultCmds(help i18n.Translator) []*cobra.Command {
	return []*cobra.Command{
		withTxFlags(&cobra.Command{Use: "team [name] [how-many-must-sign] [member]...", Short: help.T("team.short"), Args: cobra.MinimumNArgs(4), RunE: runTeam}),
		withTxFlags(&cobra.Command{Use: "vault [name] [days]", Short: help.T("vault.short"), Args: cobra.ExactArgs(2), RunE: runVault}),
		withTxFlags(&cobra.Command{Use: "lock [name] [LOCK] [until-date]", Short: help.T("lock.short"), Args: cobra.ExactArgs(3), RunE: runLock}),
		withTxFlags(&cobra.Command{Use: "locks [name-or-address]", Short: help.T("locks.short"), Args: cobra.ExactArgs(1), RunE: runLocks}),
		withTxFlags(&cobra.Command{Use: "reserves [name-or-address] [until-date]", Short: help.T("reserves.short"), Args: cobra.ExactArgs(2), RunE: runReserves}),
	}
}

// parseDate reads YYYY-MM-DD as the end of that day (23:59:59, local time).
func parseDate(tr i18n.Translator, s string) (int64, error) {
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return 0, errors.New(tr.T("err.date", s))
	}
	return d.Add(24*time.Hour - time.Second).Unix(), nil
}

// runTeam saves a team account (M of N existing hybrid wallets) in the
// keyring. Nothing is sent to the network: the account exists once it
// receives coins or registers.
func runTeam(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	threshold, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	var members []cryptotypes.PubKey
	for _, name := range args[2:] {
		rec, err := clientCtx.Keyring.Key(name)
		if err != nil {
			return errors.New(tr.T("err.no_wallet", name))
		}
		pk, err := rec.GetPubKey()
		if err != nil {
			return err
		}
		members = append(members, pk)
	}
	team := multisig.NewLegacyAminoPubKey(threshold, members)
	if !hybrid.IsTeam(team) {
		return errors.New(tr.T("chain.recovery.2"))
	}
	rec, err := clientCtx.Keyring.SaveMultisig(args[0], team)
	if err != nil {
		return err
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return err
	}
	cmd.Println(tr.T("team.done", args[0], threshold, len(members), addr))
	return nil
}

func runVault(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	days, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &transferstypes.MsgSetVault{Owner: owner.String(), DelayDays: uint32(days)}); err != nil {
		return err
	}
	s, err := shieldOf(cmd, clientCtx, owner.String())
	if err != nil {
		return err
	}
	if s.VaultDelay == 0 {
		cmd.Println(tr.T("vault.off", args[0]))
	} else {
		cmd.Println(tr.T("vault.done", args[0], s.VaultDelay/(24*60*60)))
	}
	if s.VaultDelayChangeAt != 0 {
		cmd.Println(tr.T("vault.scheduled", when(s.VaultDelayChangeAt)))
	}
	return nil
}

func runLock(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	amount, err := wallet.ParseLOCK(args[1])
	if err != nil {
		return errors.New(tr.T("err.amount", args[1]))
	}
	until, err := parseDate(tr, args[2])
	if err != nil {
		return err
	}
	res, err := broadcast(cmd, clientCtx, tr, args[0], &vaulttypes.MsgLock{Owner: owner.String(), Amount: amount, Until: until})
	if err != nil {
		return err
	}
	id, _ := strconv.ParseUint(eventAttributes(res, "lock_created")["id"], 10, 64)
	cmd.Println(tr.T("lock.done", id, wallet.FormatLOCK(amount), when(until)))
	return nil
}

func runLocks(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	res, err := vaulttypes.NewQueryClient(clientCtx).Locks(cmd.Context(), &vaulttypes.QueryLocksRequest{Address: addr.String()})
	if err != nil {
		return err
	}
	cmd.Println(tr.T("locks.header", args[0]))
	for _, l := range res.Locks {
		cmd.Println(tr.T("locks.line", l.Id, wallet.FormatLOCK(l.Amount), when(l.Until)))
	}
	if len(res.Locks) == 0 {
		cmd.Println(tr.T("pending.none"))
	}
	return nil
}

func runReserves(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	until, err := parseDate(tr, args[1])
	if err != nil {
		return err
	}
	res, err := vaulttypes.NewQueryClient(clientCtx).Reserves(cmd.Context(), &vaulttypes.QueryReservesRequest{Address: addr.String(), Until: until})
	if err != nil {
		return err
	}
	cmd.Println(tr.T("reserves.line", args[0], wallet.FormatLOCK(res.Amount), when(until)))
	return nil
}
