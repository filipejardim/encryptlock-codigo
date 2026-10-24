package cmd

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"

	"encryptlock/lockkeeper/i18n"
	"encryptlock/lockkeeper/wallet"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
)

// shieldCmds are the security shield commands of `lockkeeper wallet`
// (white paper, section 7.2).
func shieldCmds(help i18n.Translator) []*cobra.Command {
	return []*cobra.Command{
		withTxFlags(&cobra.Command{Use: "device [name] [device-name-or-address|none]", Short: help.T("device.short"), Args: cobra.ExactArgs(2), RunE: runDevice}),
		withTxFlags(&cobra.Command{Use: "limits [name] [daily-LOCK] [approval-above-LOCK]", Short: help.T("limits.short"), Args: cobra.ExactArgs(3), RunE: runLimits}),
		withTxFlags(&cobra.Command{Use: "not-me [signer] [account]", Aliases: []string{"freeze"}, Short: help.T("freeze.short"), Args: cobra.ExactArgs(2), RunE: runFreeze}),
		withTxFlags(&cobra.Command{Use: "unfreeze [signer] [account]", Short: help.T("unfreeze.short"), Args: cobra.ExactArgs(2), RunE: runUnfreeze}),
		withTxFlags(&cobra.Command{Use: "approve [device-name] [id]", Short: help.T("approve.short"), Args: cobra.ExactArgs(2), RunE: runApprove}),
		withTxFlags(&cobra.Command{Use: "shield [name-or-address]", Short: help.T("shield.short"), Args: cobra.ExactArgs(1), RunE: runShield}),
	}
}

func when(unix int64) string {
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04:05")
}

// limitText shows a limit in LOCK, or "none" for 0.
func limitText(tr i18n.Translator, units uint64) string {
	if units == 0 {
		return tr.T("value.none")
	}
	return wallet.FormatLOCK(units) + " LOCK"
}

// parseLimit reads a limit in LOCK; "0" means none.
func parseLimit(s string) (uint64, error) {
	if strings.TrimSpace(s) == "0" {
		return 0, nil
	}
	return wallet.ParseLOCK(s)
}

func runDevice(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	device := ""
	if !strings.EqualFold(args[1], "none") {
		addr, err := address(clientCtx, tr, args[1])
		if err != nil {
			return err
		}
		device = addr.String()
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &transferstypes.MsgSetDevice{Owner: owner.String(), Device: device}); err != nil {
		return err
	}
	s, err := shieldOf(cmd, clientCtx, owner.String())
	if err != nil {
		return err
	}
	if s.DeviceChangePending {
		cmd.Println(tr.T("device.scheduled", args[0], deviceText(tr, s.PendingDevice), when(s.DeviceChangeAt)))
		return nil
	}
	cmd.Println(tr.T("device.done", args[0], deviceText(tr, s.Device)))
	return nil
}

func deviceText(tr i18n.Translator, device string) string {
	if device == "" {
		return tr.T("value.none")
	}
	return device
}

func runLimits(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	daily, err := parseLimit(args[1])
	if err != nil {
		return err
	}
	above, err := parseLimit(args[2])
	if err != nil {
		return err
	}
	msg := &transferstypes.MsgSetLimits{Owner: owner.String(), DailyLimit: daily, ApprovalAbove: above}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], msg); err != nil {
		return err
	}
	s, err := shieldOf(cmd, clientCtx, owner.String())
	if err != nil {
		return err
	}
	cmd.Println(tr.T("limits.done", limitText(tr, s.DailyLimit), limitText(tr, s.ApprovalAbove)))
	if at := max(s.DailyLimitChangeAt, s.ApprovalAboveChangeAt); at != 0 {
		cmd.Println(tr.T("limits.scheduled", when(at)))
	}
	return nil
}

func runFreeze(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	signer, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	account, err := address(clientCtx, tr, args[1])
	if err != nil {
		return err
	}
	res, err := broadcast(cmd, clientCtx, tr, args[0], &transferstypes.MsgFreeze{Signer: signer.String(), Account: account.String()})
	if err != nil {
		return err
	}
	canceled := 0
	for _, ev := range res.Events {
		if ev.Type == "transfer_canceled" {
			canceled++
		}
	}
	cmd.Println(tr.T("freeze.done", args[1], canceled))
	return nil
}

func runUnfreeze(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	signer, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	account, err := address(clientCtx, tr, args[1])
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &transferstypes.MsgUnfreeze{Signer: signer.String(), Account: account.String()}); err != nil {
		return err
	}
	s, err := shieldOf(cmd, clientCtx, account.String())
	if err != nil {
		return err
	}
	if !s.Frozen {
		cmd.Println(tr.T("unfreeze.done", args[1]))
		return nil
	}
	first := s.UnfreezeOwnerAt
	if first == 0 || (s.UnfreezeDeviceAt != 0 && s.UnfreezeDeviceAt < first) {
		first = s.UnfreezeDeviceAt
	}
	cmd.Println(tr.T("unfreeze.scheduled", args[1], when(first+int64((7*24*time.Hour).Seconds()))))
	return nil
}

func runApprove(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	device, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(args[1], "#"), 10, 64)
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &transferstypes.MsgApprove{Device: device.String(), Id: id}); err != nil {
		return err
	}
	cmd.Println(tr.T("approve.done", id))
	return nil
}

func runShield(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	s, err := shieldOf(cmd, clientCtx, addr.String())
	if err != nil {
		return err
	}
	cmd.Println(tr.T("shield.header", args[0]))
	cmd.Println(tr.T("shield.device", deviceText(tr, s.Device)))
	if s.DeviceChangePending {
		cmd.Println(tr.T("shield.device_change", deviceText(tr, s.PendingDevice), when(s.DeviceChangeAt)))
	}
	cmd.Println(tr.T("shield.daily", limitText(tr, s.DailyLimit), wallet.FormatLOCK(s.WindowSpent)))
	if s.DailyLimitChangeAt != 0 {
		cmd.Println(tr.T("shield.change", limitText(tr, s.PendingDailyLimit), when(s.DailyLimitChangeAt)))
	}
	cmd.Println(tr.T("shield.approval", limitText(tr, s.ApprovalAbove)))
	if s.ApprovalAboveChangeAt != 0 {
		cmd.Println(tr.T("shield.change", limitText(tr, s.PendingApprovalAbove), when(s.ApprovalAboveChangeAt)))
	}
	if s.Frozen {
		cmd.Println(tr.T("shield.frozen", when(s.FrozenAt)))
	} else {
		cmd.Println(tr.T("shield.not_frozen"))
	}
	return nil
}

func shieldOf(cmd *cobra.Command, clientCtx client.Context, addr string) (*transferstypes.Shield, error) {
	res, err := transferstypes.NewQueryClient(clientCtx).Shield(cmd.Context(), &transferstypes.QueryShieldRequest{Address: addr})
	if err != nil {
		return nil, err
	}
	return res.Shield, nil
}
