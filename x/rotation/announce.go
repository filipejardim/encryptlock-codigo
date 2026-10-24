package rotation

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	mldsa "github.com/cometbft/cometbft/crypto/mldsa65"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/rotation/types"
)

// announceLife: an announced key waits this long for a phone to link it.
const announceLife = 24 * 60 * 60

// AnnounceKey records the block-signing key of a computer that wants to be
// linked from a phone by QR code (the key is too big for the code itself).
func (k Keeper) AnnounceKey(ctx context.Context, helper sdk.AccAddress, pubKey []byte) error {
	if len(pubKey) != mldsa.PubKeySize {
		return errorsmod.Wrapf(types.ErrBadKey, "want %d bytes, got %d", mldsa.PubKeySize, len(pubKey))
	}
	if old, err := k.Announced.Get(ctx, helper); err == nil {
		if err := k.AnnouncedAt.Remove(ctx, collections.Join(old.At, helper)); err != nil {
			return err
		}
	}
	t := now(ctx)
	if err := k.AnnouncedAt.Set(ctx, collections.Join(t, helper)); err != nil {
		return err
	}
	return k.Announced.Set(ctx, helper, types.Announced{PubKey: pubKey, At: t})
}

// pruneAnnounced deletes keys announced more than 24 hours ago (at most 100
// per block).
func (k Keeper) pruneAnnounced(ctx context.Context) error {
	iter, err := k.AnnouncedAt.Iterate(ctx, collections.NewPrefixUntilPairRange[int64, sdk.AccAddress](now(ctx)-announceLife))
	if err != nil {
		return err
	}
	keys, err := iter.Keys()
	if err != nil {
		return err
	}
	for i, key := range keys {
		if i == 100 {
			break
		}
		if err := k.AnnouncedAt.Remove(ctx, key); err != nil {
			return err
		}
		if err := k.Announced.Remove(ctx, key.K2()); err != nil {
			return err
		}
	}
	return nil
}

// HelperInfo returns the key a computer announced (if still fresh) and the
// LockKeeper account it already works for (empty if none).
func (k Keeper) HelperInfo(ctx context.Context, helper sdk.AccAddress) (pubKey []byte, keeper sdk.AccAddress, err error) {
	a, err := k.Announced.Get(ctx, helper)
	if err == nil && now(ctx)-a.At <= announceLife {
		pubKey = a.PubKey
	} else if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, nil, err
	}
	owner, err := k.Helpers.Get(ctx, helper)
	if err == nil {
		keeper = sdk.AccAddress(owner)
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, nil, err
	}
	return pubKey, keeper, nil
}

func (s msgServer) AnnounceKey(ctx context.Context, msg *types.MsgAnnounceKey) (*types.MsgAnnounceKeyResponse, error) {
	helper, err := s.k.addressCodec.StringToBytes(msg.Helper)
	if err != nil {
		return nil, err
	}
	if err := s.k.AnnounceKey(ctx, helper, msg.PubKey); err != nil {
		return nil, err
	}
	return &types.MsgAnnounceKeyResponse{}, nil
}

func (s queryServer) Helper(ctx context.Context, req *types.QueryHelperRequest) (*types.QueryHelperResponse, error) {
	helper, err := s.k.addressCodec.StringToBytes(req.Helper)
	if err != nil {
		return nil, err
	}
	key, keeper, err := s.k.HelperInfo(ctx, helper)
	if err != nil {
		return nil, err
	}
	res := &types.QueryHelperResponse{PubKey: key}
	if res.Chip, err = s.k.ChipOf.Get(ctx, helper); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	if len(keeper) > 0 {
		if res.Keeper, err = s.k.addressCodec.BytesToString(keeper); err != nil {
			return nil, err
		}
	}
	return res, nil
}
