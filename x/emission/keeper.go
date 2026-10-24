package emission

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/protocol"
)

// ModuleName is also the name of the module account that mints new coins.
const ModuleName = "emission"

// BankKeeper is the part of the bank module this module uses.
type BankKeeper interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// Keeper stores when the network started and how much has been emitted.
type Keeper struct {
	bank         BankKeeper
	denom        string
	feeCollector string

	// StartTime is the genesis time, in Unix seconds of network time.
	StartTime collections.Item[int64]
	// Emitted is the total lockunit emitted since genesis (excluding the
	// genesis allocation).
	Emitted collections.Item[uint64]
}

// NewKeeper creates the emission keeper. New coins go to feeCollector; from
// there x/rotation shares them every hour among all LockKeepers switched on,
// by time on × guarantee, with the 5% cap.
func NewKeeper(storeService store.KVStoreService, bank BankKeeper, denom, feeCollector string) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		bank:         bank,
		denom:        denom,
		feeCollector: feeCollector,
		StartTime:    collections.NewItem(sb, collections.NewPrefix(0), "start_time", collections.Int64Value),
		Emitted:      collections.NewItem(sb, collections.NewPrefix(1), "emitted", collections.Uint64Value),
	}
	if _, err := sb.Build(); err != nil {
		panic(err)
	}
	return k
}

// BeginBlock releases whatever the schedule says is due by this block's time.
func (k Keeper) BeginBlock(ctx context.Context) error {
	start, err := k.StartTime.Get(ctx)
	if err != nil {
		return err
	}
	emitted, err := k.Emitted.Get(ctx)
	if err != nil {
		return err
	}

	elapsed := sdk.UnwrapSDKContext(ctx).BlockTime().Unix() - start
	due := CumulativeTarget(elapsed)
	if due <= emitted {
		return nil
	}
	amount := due - emitted

	// Golden rule: never more than 40.000.000 LOCK. Unreachable by design
	// (the schedule adds up to exactly the cap); checked anyway.
	if GenesisAllocation+due > protocol.MaxSupplyBaseUnits {
		return fmt.Errorf("emission would exceed the maximum supply: %d > %d", GenesisAllocation+due, protocol.MaxSupplyBaseUnits)
	}

	coins := sdk.NewCoins(sdk.NewCoin(k.denom, sdkmath.NewIntFromUint64(amount)))
	if err := k.bank.MintCoins(ctx, ModuleName, coins); err != nil {
		return err
	}
	if err := k.bank.SendCoinsFromModuleToModule(ctx, ModuleName, k.feeCollector, coins); err != nil {
		return err
	}
	return k.Emitted.Set(ctx, due)
}

// GenesisTime returns the network's start time (Unix seconds).
func (k Keeper) GenesisTime(ctx context.Context) (int64, error) {
	return k.StartTime.Get(ctx)
}
