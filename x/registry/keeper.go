// Package registry keeps the list of registered accounts. Only registered
// accounts can receive transfers (white paper, sections 6.1 and 7.1).
package registry

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/registry/types"
)

// Keeper stores registered accounts and when they registered.
type Keeper struct {
	addressCodec address.Codec

	// Registered maps an account to its registration time (Unix seconds).
	Registered collections.Map[sdk.AccAddress, int64]
	// Fair use (fairuse.go): transactions per (hour, account), new accounts
	// in the current block, and registrations by time (to forget unused
	// ones).
	TxCount   collections.Map[collections.Pair[int64, sdk.AccAddress], uint64]
	RegHeight collections.Item[int64]
	RegCount  collections.Item[uint64]
	ByTime    collections.KeySet[collections.Pair[int64, sdk.AccAddress]]
	deps      *pruneDeps
}

// NewKeeper creates the registry keeper.
func NewKeeper(storeService store.KVStoreService, addressCodec address.Codec) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		addressCodec: addressCodec,
		Registered:   collections.NewMap(sb, collections.NewPrefix(0), "registered", sdk.AccAddressKey, collections.Int64Value),
		TxCount:      collections.NewMap(sb, collections.NewPrefix(1), "tx_count", collections.PairKeyCodec(collections.Int64Key, sdk.AccAddressKey), collections.Uint64Value),
		RegHeight:    collections.NewItem(sb, collections.NewPrefix(2), "reg_height", collections.Int64Value),
		RegCount:     collections.NewItem(sb, collections.NewPrefix(3), "reg_count", collections.Uint64Value),
		ByTime:       collections.NewKeySet(sb, collections.NewPrefix(4), "by_time", collections.PairKeyCodec(collections.Int64Key, sdk.AccAddressKey)),
		deps:         &pruneDeps{},
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

// IsRegistered reports whether addr is a registered account.
func (k Keeper) IsRegistered(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	return k.Registered.Has(ctx, addr)
}

// Register records addr. Each account can register only once.
func (k Keeper) Register(ctx context.Context, addr sdk.AccAddress) error {
	has, err := k.Registered.Has(ctx, addr)
	if err != nil {
		return err
	}
	if has {
		return types.ErrAlreadyRegistered
	}
	t := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if err := k.Registered.Set(ctx, addr, t); err != nil {
		return err
	}
	// Forgotten after a while if it is never used (fairuse.go).
	return k.ByTime.Set(ctx, collections.Join(t, addr))
}

type msgServer struct{ k Keeper }

func (s msgServer) Register(ctx context.Context, msg *types.MsgRegister) (*types.MsgRegisterResponse, error) {
	addr, err := s.k.addressCodec.StringToBytes(msg.Address)
	if err != nil {
		return nil, err
	}
	if err := s.k.Register(ctx, addr); err != nil {
		return nil, err
	}
	return &types.MsgRegisterResponse{}, nil
}

type queryServer struct{ k Keeper }

func (s queryServer) Registered(ctx context.Context, req *types.QueryRegisteredRequest) (*types.QueryRegisteredResponse, error) {
	addr, err := s.k.addressCodec.StringToBytes(req.Address)
	if err != nil {
		return nil, err
	}
	at, err := s.k.Registered.Get(ctx, addr)
	if errors.Is(err, collections.ErrNotFound) {
		return &types.QueryRegisteredResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &types.QueryRegisteredResponse{Registered: true, RegisteredAt: at}, nil
}
