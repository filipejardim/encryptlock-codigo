package rotation

import (
	"context"
	"encoding/json"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"encryptlock/lockkeeper/x/rotation/types"
)

var (
	_ module.AppModuleBasic     = AppModule{}
	_ module.HasGenesis         = AppModule{}
	_ module.HasServices        = AppModule{}
	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
)

// AppModule plugs rotation into the application. Its state (who is on, the
// signing group) starts empty and fills itself; genesis carries only the chip
// checkers and the computers that start the network (chip.go).
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the rotation module.
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

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage { return json.RawMessage(`{}`) }

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	_, err := ParseGenesis(bz)
	return err
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	g, err := ParseGenesis(bz)
	if err == nil {
		err = am.keeper.InitGenesis(ctx, g)
	}
	if err != nil {
		panic(err)
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	g, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	bz, err := json.Marshal(g)
	if err != nil {
		panic(err)
	}
	return bz
}

func (am AppModule) BeginBlock(ctx context.Context) error { return am.keeper.BeginBlock(ctx) }

// AutoCLIOptions gives the commands
//
//	lockkeeper tx rotation heartbeat --from <name>
//	lockkeeper query rotation status <address>
//	lockkeeper query rotation overview
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "encryptlock.rotation.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{{
				RpcMethod: "Heartbeat",
				Use:       "heartbeat",
				Short:     "Tell the network this LockKeeper is on (sent by itself every 5 minutes)",
			}, {
				RpcMethod: "AnnounceKey",
				Skip:      true, // sent by pc-heartbeat
			}, {
				RpcMethod: "ProveChip",
				Skip:      true, // sent by pc-heartbeat with the checker's ticket
			}, {
				RpcMethod:      "SetHelper",
				Use:            "set-helper [helper]",
				Short:          "Let a helper account send this LockKeeper's heartbeats (empty removes it)",
				PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "helper"}},
			}},
		},
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "encryptlock.rotation.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:      "Status",
					Use:            "status [address]",
					Short:          "Show whether a LockKeeper is on and signing blocks",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "keeper"}},
				},
				{RpcMethod: "Overview", Use: "overview", Short: "How many LockKeepers are on and how many sign the blocks"},
			},
		},
	}
}
