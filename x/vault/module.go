package vault

import (
	"context"
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"encryptlock/lockkeeper/x/vault/types"
)

var (
	_ module.AppModuleBasic   = AppModule{}
	_ module.HasGenesis       = AppModule{}
	_ module.HasServices      = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// GenesisState holds the locks.
type GenesisState struct {
	NextID uint64       `json:"next_id"`
	Locks  []types.Lock `json:"locks"`
}

// AppModule plugs the vault keeper into the application.
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the vault module.
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

// EndBlock gives back the locks whose date has come.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.ReleaseDue(ctx)
}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	bz, _ := json.Marshal(GenesisState{NextID: 1, Locks: []types.Lock{}})
	return bz
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("vault genesis: %w", err)
	}
	if gs.NextID < 1 {
		return fmt.Errorf("vault genesis: next_id must be at least 1")
	}
	return nil
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		panic(err)
	}
	if err := am.keeper.NextID.Set(ctx, gs.NextID); err != nil {
		panic(err)
	}
	for _, l := range gs.Locks {
		owner, err := am.keeper.addressCodec.StringToBytes(l.Owner)
		if err != nil {
			panic(err)
		}
		if err := am.keeper.save(ctx, l, owner); err != nil {
			panic(err)
		}
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	next, err := am.keeper.NextID.Peek(ctx)
	if err != nil {
		panic(err)
	}
	gs := GenesisState{NextID: next, Locks: []types.Lock{}}
	err = am.keeper.Locks.Walk(ctx, nil, func(_ uint64, l types.Lock) (bool, error) {
		gs.Locks = append(gs.Locks, l)
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

type msgServer struct{ k Keeper }

func (s msgServer) Lock(ctx context.Context, msg *types.MsgLock) (*types.MsgLockResponse, error) {
	owner, err := s.k.addressCodec.StringToBytes(msg.Owner)
	if err != nil {
		return nil, err
	}
	id, err := s.k.Lock(ctx, owner, msg.Amount, msg.Until)
	if err != nil {
		return nil, err
	}
	return &types.MsgLockResponse{Id: id}, nil
}

type queryServer struct{ k Keeper }

func (s queryServer) Locks(ctx context.Context, req *types.QueryLocksRequest) (*types.QueryLocksResponse, error) {
	owner, err := s.k.addressCodec.StringToBytes(req.Address)
	if err != nil {
		return nil, err
	}
	locks, err := s.k.LocksOf(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Lock, len(locks))
	for i := range locks {
		out[i] = &locks[i]
	}
	return &types.QueryLocksResponse{Locks: out}, nil
}

func (s queryServer) Reserves(ctx context.Context, req *types.QueryReservesRequest) (*types.QueryReservesResponse, error) {
	owner, err := s.k.addressCodec.StringToBytes(req.Address)
	if err != nil {
		return nil, err
	}
	amount, err := s.k.Reserves(ctx, owner, req.Until)
	if err != nil {
		return nil, err
	}
	return &types.QueryReservesResponse{Amount: amount}, nil
}
