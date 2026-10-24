package recovery

import (
	"context"
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"encryptlock/lockkeeper/x/recovery/types"
)

var (
	_ module.AppModuleBasic   = AppModule{}
	_ module.HasGenesis       = AppModule{}
	_ module.HasServices      = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// GenesisAccount is one account's recovery status.
type GenesisAccount struct {
	Address string       `json:"address"`
	Status  types.Status `json:"status"`
}

// GenesisForward is an inherited or recovered account and where what reaches
// it goes.
type GenesisForward struct {
	Address string     `json:"address"`
	To      types.Plan `json:"to"`
}

// GenesisState lists the accounts with a plan.
type GenesisState struct {
	Accounts []GenesisAccount `json:"accounts"`
	Forwards []GenesisForward `json:"forwards,omitempty"`
}

// AppModule plugs the recovery keeper into the application.
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the recovery module.
func NewAppModule(k Keeper) AppModule { return AppModule{keeper: k} }

func (AppModule) IsAppModule()        {}
func (AppModule) IsOnePerModuleType() {}
func (AppModule) Name() string        { return types.ModuleName }

func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}
func (AppModule) RegisterInterfaces(r codectypes.InterfaceRegistry) { types.RegisterInterfaces(r) }

// RegisterGRPCGatewayRoutes serves the queries as REST (for the app and the explorer).
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *gwruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), msgServer{k: am.keeper})
	types.RegisterQueryServer(cfg.QueryServer(), queryServer{k: am.keeper})
}

// EndBlock moves the coins of claims whose 30 days have passed, and passes on
// what reached accounts already inherited or recovered.
func (am AppModule) EndBlock(ctx context.Context) error {
	if err := am.keeper.ExecuteDue(ctx); err != nil {
		return err
	}
	return am.keeper.ForwardDue(ctx)
}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	bz, _ := json.Marshal(GenesisState{Accounts: []GenesisAccount{}})
	return bz
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("recovery genesis: %w", err)
	}
	return nil
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		panic(err)
	}
	for _, a := range gs.Accounts {
		addr, err := am.keeper.addressCodec.StringToBytes(a.Address)
		if err != nil {
			panic(err)
		}
		if err := am.keeper.Statuses.Set(ctx, addr, a.Status); err != nil {
			panic(err)
		}
		if a.Status.Claim != nil {
			if err := am.keeper.Due.Set(ctx, collections.Join(a.Status.Claim.ExecutesAt, sdk.AccAddress(addr))); err != nil {
				panic(err)
			}
		}
	}
	for _, f := range gs.Forwards {
		addr, err := am.keeper.addressCodec.StringToBytes(f.Address)
		if err != nil {
			panic(err)
		}
		if err := am.keeper.Forwards.Set(ctx, addr, f.To); err != nil {
			panic(err)
		}
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs := GenesisState{Accounts: []GenesisAccount{}}
	err := am.keeper.Statuses.Walk(ctx, nil, func(addr sdk.AccAddress, s types.Status) (bool, error) {
		str, err := am.keeper.addressCodec.BytesToString(addr)
		gs.Accounts = append(gs.Accounts, GenesisAccount{Address: str, Status: s})
		return err != nil, err
	})
	if err != nil {
		panic(err)
	}
	err = am.keeper.Forwards.Walk(ctx, nil, func(addr sdk.AccAddress, p types.Plan) (bool, error) {
		str, err := am.keeper.addressCodec.BytesToString(addr)
		gs.Forwards = append(gs.Forwards, GenesisForward{Address: str, To: p})
		return err != nil, err
	})
	if err != nil {
		panic(err)
	}
	bz, err := json.Marshal(gs)
	if err != nil {
		panic(err)
	}
	return bz
}
