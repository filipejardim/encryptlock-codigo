package emission

import (
	"context"
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

var (
	_ module.AppModuleBasic     = AppModule{}
	_ module.HasGenesis         = AppModule{}
	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
)

// GenesisState is the module's genesis. A zero StartTime means "use the
// genesis block time".
type GenesisState struct {
	StartTime int64  `json:"start_time"`
	Emitted   uint64 `json:"emitted"`
}

// AppModule plugs the emission keeper into the application.
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the emission module.
func NewAppModule(k Keeper) AppModule { return AppModule{keeper: k} }

func (AppModule) IsAppModule()        {}
func (AppModule) IsOnePerModuleType() {}
func (AppModule) Name() string        { return ModuleName }

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino)                   {}
func (AppModule) RegisterInterfaces(codectypes.InterfaceRegistry)               {}
func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	bz, _ := json.Marshal(GenesisState{})
	return bz
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("emission genesis: %w", err)
	}
	if gs.StartTime < 0 {
		return fmt.Errorf("emission genesis: negative start_time")
	}
	if gs.Emitted > AllEmission {
		return fmt.Errorf("emission genesis: emitted %d exceeds total emission %d", gs.Emitted, AllEmission)
	}
	return nil
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		panic(err)
	}

	// Golden rule: a new network starts with exactly the 1.000 LOCK genesis
	// allocation. A restarted (exported) network can have at most genesis +
	// emitted (less if coins of cheating LockKeepers were burned). Anything
	// else would break the 40.000.000 total.
	supply := am.keeper.bank.GetSupply(ctx, am.keeper.denom).Amount
	limit := sdkmath.NewIntFromUint64(GenesisAllocation + gs.Emitted)
	if gs.Emitted == 0 && !supply.Equal(limit) || supply.GT(limit) {
		panic(fmt.Sprintf("genesis supply is %s %s, must be %s", supply, am.keeper.denom, limit))
	}

	start := gs.StartTime
	if start == 0 {
		start = ctx.BlockTime().Unix()
	}
	if err := am.keeper.StartTime.Set(ctx, start); err != nil {
		panic(err)
	}
	if err := am.keeper.Emitted.Set(ctx, gs.Emitted); err != nil {
		panic(err)
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	start, err := am.keeper.StartTime.Get(ctx)
	if err != nil {
		panic(err)
	}
	emitted, err := am.keeper.Emitted.Get(ctx)
	if err != nil {
		panic(err)
	}
	bz, err := json.Marshal(GenesisState{StartTime: start, Emitted: emitted})
	if err != nil {
		panic(err)
	}
	return bz
}

func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.BeginBlock(ctx)
}
