package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/i18n"
	recoverytypes "encryptlock/lockkeeper/x/recovery/types"
)

// recoveryCmds are the inheritance and recovery commands of
// `lockkeeper wallet` (white paper, section 7.3).
func recoveryCmds(help i18n.Translator) []*cobra.Command {
	return []*cobra.Command{
		heirsCmd(help),
		withTxFlags(&cobra.Command{Use: "guardians [name] [how-many-must-approve] [trusted-person]...", Short: help.T("guardians.short"), Args: cobra.MinimumNArgs(2), RunE: runGuardians}),
		withTxFlags(&cobra.Command{Use: "alive [name]", Short: help.T("alive.short"), Args: cobra.ExactArgs(1), RunE: runAlive}),
		withTxFlags(&cobra.Command{Use: "claim-inheritance [heir] [account]", Short: help.T("claim.short"), Args: cobra.ExactArgs(2), RunE: runClaim}),
		withTxFlags(&cobra.Command{Use: "approve-recovery [trusted-person] [account] [new-account]", Short: help.T("recover_approve.short"), Args: cobra.ExactArgs(3), RunE: runApproveRecovery}),
		withTxFlags(&cobra.Command{Use: "cancel-claims [name]", Short: help.T("cancel_claims.short"), Args: cobra.ExactArgs(1), RunE: runCancelClaims}),
		withTxFlags(&cobra.Command{Use: "recovery [name-or-address]", Short: help.T("recovery.short"), Args: cobra.ExactArgs(1), RunE: runRecoveryStatus}),
	}
}

// heirsCmd: lockkeeper wallet heirs [name] [days] [heir:percent]... [--must-confirm N]
func heirsCmd(help i18n.Translator) *cobra.Command {
	cmd := withTxFlags(&cobra.Command{Use: "heirs [name] [days] [heir:percent]...", Short: help.T("heirs.short"), Args: cobra.MinimumNArgs(2), RunE: runHeirs})
	cmd.Flags().Uint32(flagMustConfirm, 0, help.T("flag.must_confirm"))
	return cmd
}

const flagMustConfirm = "must-confirm"

func statusOf(cmd *cobra.Command, clientCtx client.Context, addr string) (*recoverytypes.Status, error) {
	res, err := recoverytypes.NewQueryClient(clientCtx).Status(cmd.Context(), &recoverytypes.QueryStatusRequest{Address: addr})
	if err != nil {
		return nil, err
	}
	return res.Status, nil
}

// currentPlan is the plan a change builds on: the pending one if any,
// otherwise the one in force.
func currentPlan(s *recoverytypes.Status) recoverytypes.Plan {
	if s.PlanChangePending && s.PendingPlan != nil {
		return *s.PendingPlan
	}
	if s.Plan != nil {
		return *s.Plan
	}
	return recoverytypes.Plan{}
}

// setPlan sends the new plan and says when it applies.
func setPlan(cmd *cobra.Command, clientCtx client.Context, tr i18n.Translator, name string, owner sdk.AccAddress, plan recoverytypes.Plan, done string) error {
	if _, err := broadcast(cmd, clientCtx, tr, name, &recoverytypes.MsgSetPlan{Owner: owner.String(), Plan: &plan}); err != nil {
		return err
	}
	cmd.Println(done)
	s, err := statusOf(cmd, clientCtx, owner.String())
	if err != nil {
		return err
	}
	if s.PlanChangePending {
		cmd.Println(tr.T("plan.scheduled", when(s.PlanChangeAt)))
	}
	return nil
}

func runHeirs(cmd *cobra.Command, args []string) error {
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
	var heirs []*recoverytypes.Heir
	for _, arg := range args[2:] {
		who, pct, ok := strings.Cut(arg, ":")
		if !ok {
			return fmt.Errorf("%q: use heir:percent", arg)
		}
		addr, err := address(clientCtx, tr, who)
		if err != nil {
			return err
		}
		percent, err := strconv.ParseUint(strings.TrimSuffix(pct, "%"), 10, 32)
		if err != nil {
			return err
		}
		heirs = append(heirs, &recoverytypes.Heir{Address: addr.String(), Percent: uint32(percent)})
	}

	s, err := statusOf(cmd, clientCtx, owner.String())
	if err != nil {
		return err
	}
	plan := currentPlan(s)
	mustConfirm, _ := cmd.Flags().GetUint32(flagMustConfirm)
	plan.Heirs, plan.InactivityDays, plan.HeirThreshold = heirs, uint32(days), mustConfirm
	if len(heirs) == 0 {
		plan.InactivityDays = 0
	}
	return setPlan(cmd, clientCtx, tr, args[0], owner, plan, tr.T("heirs.done", args[0], days))
}

func runGuardians(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	threshold, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil {
		return err
	}
	var guardians []string
	for _, who := range args[2:] {
		addr, err := address(clientCtx, tr, who)
		if err != nil {
			return err
		}
		guardians = append(guardians, addr.String())
	}

	s, err := statusOf(cmd, clientCtx, owner.String())
	if err != nil {
		return err
	}
	plan := currentPlan(s)
	plan.Guardians, plan.GuardianThreshold = guardians, uint32(threshold)
	if len(guardians) == 0 {
		plan.GuardianThreshold = 0
	}
	return setPlan(cmd, clientCtx, tr, args[0], owner, plan, tr.T("guardians.done", args[0], threshold, len(guardians)))
}

func runAlive(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &recoverytypes.MsgAlive{Owner: owner.String()}); err != nil {
		return err
	}
	cmd.Println(tr.T("alive.done", args[0]))
	return nil
}

func runClaim(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	heir, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	account, err := address(clientCtx, tr, args[1])
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &recoverytypes.MsgClaimInheritance{Heir: heir.String(), Account: account.String()}); err != nil {
		return err
	}
	s, err := statusOf(cmd, clientCtx, account.String())
	if err != nil {
		return err
	}
	if s.Claim != nil {
		cmd.Println(tr.T("claim.done", args[1], when(s.Claim.ExecutesAt)))
		return nil
	}
	required := len(s.Plan.Heirs)
	if s.Plan.HeirThreshold != 0 {
		required = int(s.Plan.HeirThreshold)
	}
	until := s.ConfirmationsStartedAt + int64((30 * 24 * time.Hour).Seconds())
	cmd.Println(tr.T("claim.progress", len(s.HeirConfirmations), required, when(until)))
	return nil
}

func runApproveRecovery(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	guardian, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	account, err := address(clientCtx, tr, args[1])
	if err != nil {
		return err
	}
	newAccount, err := address(clientCtx, tr, args[2])
	if err != nil {
		return err
	}
	msg := &recoverytypes.MsgApproveRecovery{Guardian: guardian.String(), Account: account.String(), NewAddress: newAccount.String()}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], msg); err != nil {
		return err
	}
	s, err := statusOf(cmd, clientCtx, account.String())
	if err != nil {
		return err
	}
	if s.Claim != nil {
		cmd.Println(tr.T("recover_approve.started", args[2], when(s.Claim.ExecutesAt)))
		return nil
	}
	count := 0
	for _, a := range s.Approvals {
		if a.NewAddress == newAccount.String() {
			count++
		}
	}
	cmd.Println(tr.T("recover_approve.progress", count))
	return nil
}

func runCancelClaims(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	owner, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &recoverytypes.MsgCancelClaims{Owner: owner.String()}); err != nil {
		return err
	}
	cmd.Println(tr.T("cancel_claims.done", args[0]))
	return nil
}

func runRecoveryStatus(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	s, err := statusOf(cmd, clientCtx, addr.String())
	if err != nil {
		return err
	}
	cmd.Println(tr.T("recovery.header", args[0]))
	plan := recoverytypes.Plan{}
	if s.Plan != nil {
		plan = *s.Plan
	}
	if len(plan.Heirs) == 0 && len(plan.Guardians) == 0 {
		cmd.Println(tr.T("recovery.none"))
	}
	if len(plan.Heirs) > 0 {
		cmd.Println(tr.T("recovery.heirs", plan.InactivityDays))
		for _, h := range plan.Heirs {
			cmd.Println(tr.T("recovery.heir", h.Address, h.Percent))
		}
	}
	if len(plan.Guardians) > 0 {
		cmd.Println(tr.T("recovery.guardians", plan.GuardianThreshold))
		for _, g := range plan.Guardians {
			cmd.Println(tr.T("recovery.guardian", g))
		}
	}
	if s.LastActive != 0 {
		cmd.Println(tr.T("recovery.last_active", when(s.LastActive)))
	}
	if s.PlanChangePending {
		cmd.Println(tr.T("recovery.pending_plan", when(s.PlanChangeAt)))
	}
	if s.Claim != nil {
		cmd.Println(tr.T("recovery.claim", s.Claim.Kind, when(s.Claim.ExecutesAt)))
	}
	return nil
}
