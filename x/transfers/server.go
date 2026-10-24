package transfers

import (
	"context"
	"sort"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/transfers/types"
)

type msgServer struct{ k Keeper }

func (s msgServer) Send(ctx context.Context, msg *types.MsgSend) (*types.MsgSendResponse, error) {
	sender, err := s.k.addressCodec.StringToBytes(msg.Sender)
	if err != nil {
		return nil, err
	}
	recipient, err := s.k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, err
	}
	id, err := s.k.Send(ctx, sender, recipient, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgSendResponse{Id: id}, nil
}

func (s msgServer) Accept(ctx context.Context, msg *types.MsgAccept) (*types.MsgAcceptResponse, error) {
	recipient, err := s.k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, err
	}
	if err := s.k.Accept(ctx, recipient, msg.Id); err != nil {
		return nil, err
	}
	return &types.MsgAcceptResponse{}, nil
}

func (s msgServer) Cancel(ctx context.Context, msg *types.MsgCancel) (*types.MsgCancelResponse, error) {
	sender, err := s.k.addressCodec.StringToBytes(msg.Sender)
	if err != nil {
		return nil, err
	}
	if err := s.k.Cancel(ctx, sender, msg.Id); err != nil {
		return nil, err
	}
	return &types.MsgCancelResponse{}, nil
}

type queryServer struct{ k Keeper }

// maxListed: pending transfers listed per side at most; maxScanned: looked
// at at most.
const (
	maxListed  = 200
	maxScanned = 20_000
)

func (s queryServer) Transfer(ctx context.Context, req *types.QueryTransferRequest) (*types.QueryTransferResponse, error) {
	t, err := s.k.get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &types.QueryTransferResponse{Transfer: &t}, nil
}

func (s queryServer) Pending(ctx context.Context, req *types.QueryPendingRequest) (*types.QueryPendingResponse, error) {
	addr, err := s.k.addressCodec.StringToBytes(req.Address)
	if err != nil {
		return nil, err
	}
	sent, err := s.list(ctx, s.k.BySender, addr)
	if err != nil {
		return nil, err
	}
	received, err := s.list(ctx, s.k.ByRecipient, addr)
	if err != nil {
		return nil, err
	}
	return &types.QueryPendingResponse{Sent: sent, Received: received}, nil
}

func (s queryServer) list(ctx context.Context, index collections.KeySet[collections.Pair[sdk.AccAddress, uint64]], addr sdk.AccAddress) ([]*types.Transfer, error) {
	iter, err := index.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](addr))
	if err != nil {
		return nil, err
	}
	// At most maxListed, the largest first: someone sending hundreds of tiny
	// transfers to an account can neither make its app hang nor hide a real
	// transfer among them. At most maxScanned are looked at.
	var out []*types.Transfer
	for n := 0; iter.Valid() && n < maxScanned; iter.Next() {
		n++
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return nil, err
		}
		t, err := s.k.get(ctx, key.K2())
		if err != nil {
			iter.Close()
			return nil, err
		}
		out = append(out, &t)
	}
	iter.Close()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	if len(out) > maxListed {
		out = out[:maxListed]
	}
	return out, nil
}

func (s msgServer) SetDevice(ctx context.Context, msg *types.MsgSetDevice) (*types.MsgSetDeviceResponse, error) {
	owner, err := s.k.addressCodec.StringToBytes(msg.Owner)
	if err != nil {
		return nil, err
	}
	if err := s.k.SetDevice(ctx, owner, msg.Device); err != nil {
		return nil, err
	}
	return &types.MsgSetDeviceResponse{}, nil
}

func (s msgServer) SetLimits(ctx context.Context, msg *types.MsgSetLimits) (*types.MsgSetLimitsResponse, error) {
	owner, err := s.k.addressCodec.StringToBytes(msg.Owner)
	if err != nil {
		return nil, err
	}
	if err := s.k.SetLimits(ctx, owner, msg.DailyLimit, msg.ApprovalAbove); err != nil {
		return nil, err
	}
	return &types.MsgSetLimitsResponse{}, nil
}

func (s msgServer) Freeze(ctx context.Context, msg *types.MsgFreeze) (*types.MsgFreezeResponse, error) {
	signer, account, err := s.signerAndAccount(msg.Signer, msg.Account)
	if err != nil {
		return nil, err
	}
	canceled, err := s.k.Freeze(ctx, signer, account)
	if err != nil {
		return nil, err
	}
	return &types.MsgFreezeResponse{Canceled: canceled}, nil
}

func (s msgServer) Unfreeze(ctx context.Context, msg *types.MsgUnfreeze) (*types.MsgUnfreezeResponse, error) {
	signer, account, err := s.signerAndAccount(msg.Signer, msg.Account)
	if err != nil {
		return nil, err
	}
	unfrozen, at, err := s.k.Unfreeze(ctx, signer, account)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnfreezeResponse{Unfrozen: unfrozen, UnfreezesAt: at}, nil
}

func (s msgServer) Approve(ctx context.Context, msg *types.MsgApprove) (*types.MsgApproveResponse, error) {
	device, err := s.k.addressCodec.StringToBytes(msg.Device)
	if err != nil {
		return nil, err
	}
	if err := s.k.Approve(ctx, device, msg.Id); err != nil {
		return nil, err
	}
	return &types.MsgApproveResponse{}, nil
}

func (s msgServer) signerAndAccount(signerStr, accountStr string) (sdk.AccAddress, sdk.AccAddress, error) {
	signer, err := s.k.addressCodec.StringToBytes(signerStr)
	if err != nil {
		return nil, nil, err
	}
	account, err := s.k.addressCodec.StringToBytes(accountStr)
	if err != nil {
		return nil, nil, err
	}
	return signer, account, nil
}

func (s queryServer) Shield(ctx context.Context, req *types.QueryShieldRequest) (*types.QueryShieldResponse, error) {
	addr, err := s.k.addressCodec.StringToBytes(req.Address)
	if err != nil {
		return nil, err
	}
	shield, err := s.k.GetShield(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &types.QueryShieldResponse{Shield: &shield}, nil
}

func (s queryServer) DevFund(ctx context.Context, _ *types.QueryDevFundRequest) (*types.QueryDevFundResponse, error) {
	addr, err := s.k.DevFundAddress(ctx)
	if err != nil {
		return nil, err
	}
	str, err := s.k.addressCodec.BytesToString(addr)
	if err != nil {
		return nil, err
	}
	return &types.QueryDevFundResponse{Address: str}, nil
}

func (s msgServer) SetVault(ctx context.Context, msg *types.MsgSetVault) (*types.MsgSetVaultResponse, error) {
	owner, err := s.k.addressCodec.StringToBytes(msg.Owner)
	if err != nil {
		return nil, err
	}
	if err := s.k.SetVault(ctx, owner, msg.DelayDays); err != nil {
		return nil, err
	}
	return &types.MsgSetVaultResponse{}, nil
}
