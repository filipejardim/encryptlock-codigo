package transfers

import (
	"context"
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/collections"
	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"encryptlock/lockkeeper/x/transfers/types"
)

var (
	_ module.AppModuleBasic   = AppModule{}
	_ module.HasGenesis       = AppModule{}
	_ module.HasServices      = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// GenesisState holds the development fund address (fixed forever at genesis)
// and the pending transfers (normally none at genesis).
type GenesisState struct {
	DevFundAddress string           `json:"dev_fund_address"`
	NextID         uint64           `json:"next_id"`
	Transfers      []types.Transfer `json:"transfers"`
	Shields        []GenesisShield  `json:"shields"`
	Contacts       []GenesisContact `json:"contacts"`
}

// GenesisShield is one account's security settings.
type GenesisShield struct {
	Address string       `json:"address"`
	Shield  types.Shield `json:"shield"`
}

// GenesisContact records that sender has had a transfer accepted by recipient.
type GenesisContact struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
}

// AppModule plugs the transfers keeper into the application.
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the transfers module.
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

// EndBlock refunds every transfer not accepted within 1 hour.
func (am AppModule) EndBlock(ctx context.Context) error {
	_, err := am.keeper.ExpireDue(ctx)
	return err
}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	// Transfer numbers start at 1 (0 would be hidden in JSON output).
	bz, _ := json.Marshal(GenesisState{NextID: 1, Transfers: []types.Transfer{}})
	return bz
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("transfers genesis: %w", err)
	}
	if gs.DevFundAddress == "" {
		return fmt.Errorf("transfers genesis: dev_fund_address is required (development fund, white paper section 8.3)")
	}
	if gs.NextID < 1 {
		return fmt.Errorf("transfers genesis: next_id must be at least 1")
	}
	for _, t := range gs.Transfers {
		if t.Id >= gs.NextID {
			return fmt.Errorf("transfers genesis: transfer %d not below next_id %d", t.Id, gs.NextID)
		}
		if t.Amount == 0 {
			return fmt.Errorf("transfers genesis: transfer %d has no amount", t.Id)
		}
	}
	return nil
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		panic(err)
	}
	devFund, err := am.keeper.addressCodec.StringToBytes(gs.DevFundAddress)
	if err != nil {
		panic(fmt.Errorf("transfers genesis: dev_fund_address: %w", err))
	}
	if err := am.keeper.DevFund.Set(ctx, devFund); err != nil {
		panic(err)
	}
	if err := am.keeper.NextID.Set(ctx, gs.NextID); err != nil {
		panic(err)
	}
	for _, t := range gs.Transfers {
		sender, err := am.keeper.addressCodec.StringToBytes(t.Sender)
		if err != nil {
			panic(err)
		}
		recipient, err := am.keeper.addressCodec.StringToBytes(t.Recipient)
		if err != nil {
			panic(err)
		}
		if err := am.keeper.save(ctx, t, sender, recipient); err != nil {
			panic(err)
		}
	}
	for _, s := range gs.Shields {
		addr, err := am.keeper.addressCodec.StringToBytes(s.Address)
		if err != nil {
			panic(err)
		}
		if err := am.keeper.Shields.Set(ctx, addr, s.Shield); err != nil {
			panic(err)
		}
	}
	for _, c := range gs.Contacts {
		sender, err := am.keeper.addressCodec.StringToBytes(c.Sender)
		if err != nil {
			panic(err)
		}
		recipient, err := am.keeper.addressCodec.StringToBytes(c.Recipient)
		if err != nil {
			panic(err)
		}
		if err := am.keeper.Contacts.Set(ctx, contactKey(sender, recipient)); err != nil {
			panic(err)
		}
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	next, err := am.keeper.NextID.Peek(ctx)
	if err != nil {
		panic(err)
	}
	devFund, err := am.keeper.DevFund.Get(ctx)
	if err != nil {
		panic(err)
	}
	devFundStr, err := am.keeper.addressCodec.BytesToString(devFund)
	if err != nil {
		panic(err)
	}
	gs := GenesisState{DevFundAddress: devFundStr, NextID: next, Transfers: []types.Transfer{}, Shields: []GenesisShield{}, Contacts: []GenesisContact{}}
	err = am.keeper.Pending.Walk(ctx, nil, func(_ uint64, t types.Transfer) (bool, error) {
		gs.Transfers = append(gs.Transfers, t)
		return false, nil
	})
	if err != nil {
		panic(err)
	}
	err = am.keeper.Shields.Walk(ctx, nil, func(addr sdk.AccAddress, s types.Shield) (bool, error) {
		str, err := am.keeper.addressCodec.BytesToString(addr)
		gs.Shields = append(gs.Shields, GenesisShield{Address: str, Shield: s})
		return err != nil, err
	})
	if err != nil {
		panic(err)
	}
	err = am.keeper.Contacts.Walk(ctx, nil, func(key collections.Pair[sdk.AccAddress, sdk.AccAddress]) (bool, error) {
		sender, err := am.keeper.addressCodec.BytesToString(key.K1())
		if err != nil {
			return true, err
		}
		recipient, err := am.keeper.addressCodec.BytesToString(key.K2())
		gs.Contacts = append(gs.Contacts, GenesisContact{Sender: sender, Recipient: recipient})
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

// AutoCLIOptions gives the commands
//
//	lockkeeper tx transfers send [recipient] [amount-in-lockunit] --from <address>
//	lockkeeper tx transfers accept [id] --from <address>
//	lockkeeper tx transfers cancel [id] --from <address>
//	lockkeeper query transfers transfer [id]
//	lockkeeper query transfers pending [address]
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: "encryptlock.transfers.v1.Msg",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:      "Send",
					Use:            "send [recipient] [amount]",
					Short:          "Send lockunit to a registered account; it must accept within 1 hour",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "recipient"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "Accept",
					Use:            "accept [id]",
					Short:          "Accept a transfer sent to you",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "Cancel",
					Use:            "cancel [id]",
					Short:          "Cancel a transfer you sent, while it is pending",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
			},
		},
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: "encryptlock.transfers.v1.Query",
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:      "Transfer",
					Use:            "transfer [id]",
					Short:          "Show a pending transfer",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "Pending",
					Use:            "pending [address]",
					Short:          "List pending transfers sent and received by an account",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
			},
		},
	}
}
