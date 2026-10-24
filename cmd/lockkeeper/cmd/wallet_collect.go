package cmd

import (
	"errors"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/cobra"

	"encryptlock/lockkeeper/i18n"
	"encryptlock/lockkeeper/protocol"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
)

// collectEvery is how often a LockKeeper's earnings go into its balance
// (founder's decision: every hour, not configurable).
const collectEvery = time.Hour

func autoCollectCmd(help i18n.Translator) *cobra.Command {
	return withTxFlags(&cobra.Command{
		Use:   "auto-collect [name]",
		Short: help.T("autocollect.short"),
		Args:  cobra.ExactArgs(1),
		RunE:  runAutoCollect,
	})
}

// runAutoCollect runs next to a LockKeeper: right away and then every hour it
// moves the LockKeeper's rewards and commission into its balance, if there
// is anything to move. It never stops on a failed round (the network may be
// starting); it simply tries again at the next round.
func runAutoCollect(cmd *cobra.Command, args []string) error {
	_, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	name := args[0]
	cmd.Println(tr.T("autocollect.started", name))
	var lastCollect, lastBeat time.Time
	for {
		// A LockKeeper that was switched off left the validator set (losing
		// nothing); now that it is on again, bring it back.
		if back, err := returnOnce(cmd, name); err != nil {
			cmd.PrintErrln(tr.T("autoreturn.failed", err.Error()))
		} else if back {
			cmd.Println(tr.T("autoreturn.done", name))
		}
		// "I am on": everyone switched on earns. It goes through this
		// computer's LockKeeper, so it only gets out while that is running.
		if time.Since(lastBeat) >= protocol.HeartbeatEvery {
			if err := heartbeatOnce(cmd, name); err != nil {
				cmd.PrintErrln(tr.T("heartbeat.failed", err.Error()))
			} else {
				lastBeat = time.Now()
			}
		}
		if time.Since(lastCollect) >= collectEvery {
			lastCollect = time.Now()
			if moved, err := collectOnce(cmd, name); err != nil {
				cmd.PrintErrln(tr.T("autocollect.failed", err.Error()))
			} else if moved {
				cmd.Println(tr.T("rewards.done", name))
			}
		}
		select {
		case <-cmd.Context().Done():
			return nil
		case <-time.After(returnEvery):
		}
	}
}

// heartbeatOnce tells the network the LockKeeper is on.
func heartbeatOnce(cmd *cobra.Command, name string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, name)
	if err != nil {
		return err
	}
	_, err = broadcast(cmd, clientCtx, tr, name, &rotationtypes.MsgHeartbeat{Keeper: addr.String()})
	return err
}

// returnEvery is how often the program checks whether its LockKeeper left
// the validator set while it was off.
const returnEvery = time.Minute

// returnOnce asks the network to take the LockKeeper back when it left the
// validator set for being off and the 1-minute wait is over; false when
// there was nothing to do.
func returnOnce(cmd *cobra.Command, name string) (bool, error) {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return false, err
	}
	addr, err := address(clientCtx, tr, name)
	if err != nil {
		return false, err
	}
	valAddr := sdk.ValAddress(addr)
	val, err := stakingtypes.NewQueryClient(clientCtx).Validator(cmd.Context(), &stakingtypes.QueryValidatorRequest{ValidatorAddr: valAddr.String()})
	if err != nil {
		return false, err
	}
	if !val.Validator.Jailed {
		return false, nil
	}
	if err := val.Validator.UnpackInterfaces(clientCtx.InterfaceRegistry); err != nil {
		return false, err
	}
	consAddr, err := val.Validator.GetConsAddr()
	if err != nil {
		return false, err
	}
	info, err := slashingtypes.NewQueryClient(clientCtx).SigningInfo(cmd.Context(), &slashingtypes.QuerySigningInfoRequest{ConsAddress: sdk.ConsAddress(consAddr).String()})
	if err != nil {
		return false, err
	}
	if info.ValSigningInfo.Tombstoned || time.Now().Before(info.ValSigningInfo.JailedUntil) {
		return false, nil
	}
	if _, err := broadcast(cmd, clientCtx, tr, name, &slashingtypes.MsgUnjail{ValidatorAddr: valAddr.String()}); err != nil {
		return false, err
	}
	return true, nil
}

// collectOnce moves rewards and commission into the balance; false when
// there was nothing to move.
func collectOnce(cmd *cobra.Command, name string) (bool, error) {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return false, err
	}
	addr, err := address(clientCtx, tr, name)
	if err != nil {
		return false, err
	}
	valAddr := sdk.ValAddress(addr)
	q := distrtypes.NewQueryClient(clientCtx)
	moved := false
	rewards, err := q.DelegationRewards(cmd.Context(), &distrtypes.QueryDelegationRewardsRequest{
		DelegatorAddress: addr.String(), ValidatorAddress: valAddr.String(),
	})
	if err != nil {
		return false, err
	}
	if !rewards.Rewards.IsZero() && rewards.Rewards.AmountOf("lockunit").TruncateInt().IsPositive() {
		msg := &distrtypes.MsgWithdrawDelegatorReward{DelegatorAddress: addr.String(), ValidatorAddress: valAddr.String()}
		if _, err := broadcast(cmd, clientCtx, tr, name, msg); err != nil {
			return false, err
		}
		moved = true
	}
	commission, err := q.ValidatorCommission(cmd.Context(), &distrtypes.QueryValidatorCommissionRequest{ValidatorAddress: valAddr.String()})
	if err != nil {
		return moved, err
	}
	if commission.Commission.Commission.AmountOf("lockunit").TruncateInt().IsPositive() {
		msg := &distrtypes.MsgWithdrawValidatorCommission{ValidatorAddress: valAddr.String()}
		if _, err := broadcast(cmd, clientCtx, tr, name, msg); err != nil {
			return moved, err
		}
		moved = true
	}
	if !moved && rewards == nil {
		return false, errors.New("no answer")
	}
	return moved, nil
}
