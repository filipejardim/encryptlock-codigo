package recovery

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/recovery/types"
)

type msgServer struct{ k Keeper }

func (s msgServer) addrs(strs ...string) ([]sdk.AccAddress, error) {
	out := make([]sdk.AccAddress, len(strs))
	for i, str := range strs {
		bz, err := s.k.addressCodec.StringToBytes(str)
		if err != nil {
			return nil, err
		}
		out[i] = bz
	}
	return out, nil
}

func (s msgServer) SetPlan(ctx context.Context, msg *types.MsgSetPlan) (*types.MsgSetPlanResponse, error) {
	a, err := s.addrs(msg.Owner)
	if err != nil {
		return nil, err
	}
	if err := s.k.SetPlan(ctx, a[0], msg.Plan); err != nil {
		return nil, err
	}
	return &types.MsgSetPlanResponse{}, nil
}

// Alive does nothing by itself: every signed transaction is already recorded
// as a sign of life (see the ante handler).
func (s msgServer) Alive(context.Context, *types.MsgAlive) (*types.MsgAliveResponse, error) {
	return &types.MsgAliveResponse{}, nil
}

func (s msgServer) ClaimInheritance(ctx context.Context, msg *types.MsgClaimInheritance) (*types.MsgClaimInheritanceResponse, error) {
	a, err := s.addrs(msg.Heir, msg.Account)
	if err != nil {
		return nil, err
	}
	at, err := s.k.ClaimInheritance(ctx, a[0], a[1])
	if err != nil {
		return nil, err
	}
	return &types.MsgClaimInheritanceResponse{ExecutesAt: at}, nil
}

func (s msgServer) ApproveRecovery(ctx context.Context, msg *types.MsgApproveRecovery) (*types.MsgApproveRecoveryResponse, error) {
	a, err := s.addrs(msg.Guardian, msg.Account, msg.NewAddress)
	if err != nil {
		return nil, err
	}
	approvals, at, err := s.k.ApproveRecovery(ctx, a[0], a[1], a[2])
	if err != nil {
		return nil, err
	}
	return &types.MsgApproveRecoveryResponse{Approvals: approvals, ExecutesAt: at}, nil
}

func (s msgServer) CancelClaims(ctx context.Context, msg *types.MsgCancelClaims) (*types.MsgCancelClaimsResponse, error) {
	a, err := s.addrs(msg.Owner)
	if err != nil {
		return nil, err
	}
	if err := s.k.CancelClaims(ctx, a[0]); err != nil {
		return nil, err
	}
	return &types.MsgCancelClaimsResponse{}, nil
}

type queryServer struct{ k Keeper }

func (s queryServer) Status(ctx context.Context, req *types.QueryStatusRequest) (*types.QueryStatusResponse, error) {
	addr, err := s.k.addressCodec.StringToBytes(req.Address)
	if err != nil {
		return nil, err
	}
	status, _, err := s.k.GetStatus(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &types.QueryStatusResponse{Status: &status}, nil
}
