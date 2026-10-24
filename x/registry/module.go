package registry

import (
	"context"

	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"encryptlock/lockkeeper/x/registry/types"
)

var (
	_ module.AppModuleBasic = AppModule{}
	_ module.HasGenesis     = AppModule{}
	_ module.HasServices    = AppModule{}
	_ appmodule.AppModule   = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// GenesisAccount is one registered account in genesis.
type GenesisAccount struct {
	Address      string `json:"address"`
	RegisteredAt int64  `json:"registered_at"`
}

// GenesisState lists the registered accounts.
type GenesisState struct {
	Accounts []GenesisAccount `json:"accounts"`
}

// AppModule plugs the registry into the application.
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the registry module.
func NewAppModule(k Keeper) AppModule { return AppModule{keeper: k} }

// EndBlock forgets old counts and unused accounts (fairuse.go).
func (am AppModule) EndBlock(ctx context.Context) error { return am.keeper.EndBlock(ctx) }

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

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	bz, _ := json.Marshal(GenesisState{Accounts: []GenesisAccount{}})
	return bz
}

func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("registry genesis: %w", err)
	}
	seen := map[string]bool{}
	for _, a := range gs.Accounts {
		if _, err := am.keeper.addressCodec.StringToBytes(a.Address); err != nil {
			return fmt.Errorf("registry genesis: %w", err)
		}
		if seen[a.Address] {
			return fmt.Errorf("registry genesis: %s registered twice", a.Address)
		}
		seen[a.Address] = true
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
		if err := am.keeper.Registered.Set(ctx, addr, a.RegisteredAt); err != nil {
			panic(err)
		}
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs := GenesisState{Accounts: []GenesisAccount{}}
	err := am.keeper.Registered.Walk(ctx, nil, func(addr sdk.AccAddress, at int64) (bool, error) {
		s, err := am.keeper.addressCodec.BytesToString(addr)
		if err != nil {
			return true, err
		}
		gs.Accounts = append(gs.Accounts, GenesisAccount{Address: s, RegisteredAt: at})
		return false, nil
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

// AutoCLIOptions gives the commands
//
//	lockkeeper tx registry register --from <address>
//	lockkeeper query registry registered <address>
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "encryptlock.registry.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{{
				RpcMethod: "Register",
				Use:       "register",
				Short:     "Register your account so it can receive transfers (free)",
			}},
		},
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "encryptlock.registry.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{{
				RpcMethod:      "Registered",
				Use:            "registered [address]",
				Short:          "Show whether an account is registered",
				PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
			}},
		},
	}
}
