package market

import (
	"cosmossdk.io/collections"

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

	"encryptlock/lockkeeper/x/market/types"
)

var (
	_ module.AppModuleBasic   = AppModule{}
	_ module.HasGenesis       = AppModule{}
	_ module.HasServices      = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// GenesisState holds the market.
type GenesisState struct {
	NextOffer uint64        `json:"next_offer"`
	NextTrade uint64        `json:"next_trade"`
	NextFill  uint64        `json:"next_fill"`
	Offers    []types.Offer `json:"offers"`
	Trades    []types.Trade `json:"trades"`
	Fills     []types.Fill  `json:"fills"`
	// Polygon: where the USDC payments are watched (none: trades are off).
	Polygon *PolygonConfig `json:"polygon,omitempty"`
}

// AppModule plugs the market keeper into the application.
type AppModule struct {
	keeper Keeper
}

// NewAppModule creates the market module.
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

// EndBlock ends unpaid reservations and expired offers.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.EndBlock(ctx)
}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	bz, _ := json.Marshal(GenesisState{NextOffer: 1, NextTrade: 1, NextFill: 1, Offers: []types.Offer{}, Trades: []types.Trade{}, Fills: []types.Fill{}})
	return bz
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("market genesis: %w", err)
	}
	if gs.NextOffer < 1 || gs.NextTrade < 1 || gs.NextFill < 1 {
		return fmt.Errorf("market genesis: counters must start at 1")
	}
	if gs.Polygon != nil && !gs.Polygon.Valid() {
		return fmt.Errorf("market genesis: polygon needs chain_id, vault and usdc (0x addresses)")
	}
	return nil
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		panic(err)
	}
	k := am.keeper
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(k.NextOffer.Set(ctx, gs.NextOffer))
	must(k.NextTrade.Set(ctx, gs.NextTrade))
	must(k.NextFill.Set(ctx, gs.NextFill))
	for _, o := range gs.Offers {
		must(k.Offers.Set(ctx, o.Id, o))
		if maker, err := k.addressCodec.StringToBytes(o.Maker); err == nil {
			must(k.add(ctx, k.OpenOffers, maker, 1))
			must(k.ByMaker.Set(ctx, collections.Join(sdk.AccAddress(maker), o.Id)))
		}
		if o.ExpiresAt != 0 {
			must(k.OffersByExpiry.Set(ctx, pair(o.ExpiresAt, o.Id)))
		}
	}
	for _, tr := range gs.Trades {
		must(k.Trades.Set(ctx, tr.Id, tr))
		if buyer, err := k.addressCodec.StringToBytes(tr.Buyer); err == nil {
			must(k.add(ctx, k.BuyerTrades, buyer, 1))
		}
		must(k.TradesByEnd.Set(ctx, pair(tr.ReservedUntil, tr.Id)))
	}
	for i, f := range gs.Fills {
		must(k.Fills.Set(ctx, uint64(i+1), f))
	}
	if gs.Polygon != nil {
		bz, err := json.Marshal(gs.Polygon)
		must(err)
		must(k.Polygon.Set(ctx, bz))
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	k := am.keeper
	gs := GenesisState{Offers: []types.Offer{}, Trades: []types.Trade{}, Fills: []types.Fill{}}
	var err error
	if gs.NextOffer, err = k.NextOffer.Peek(ctx); err != nil {
		panic(err)
	}
	if gs.NextTrade, err = k.NextTrade.Peek(ctx); err != nil {
		panic(err)
	}
	if err := k.Offers.Walk(ctx, nil, func(_ uint64, o types.Offer) (bool, error) {
		gs.Offers = append(gs.Offers, o)
		return false, nil
	}); err != nil {
		panic(err)
	}
	if err := k.Trades.Walk(ctx, nil, func(_ uint64, tr types.Trade) (bool, error) {
		gs.Trades = append(gs.Trades, tr)
		return false, nil
	}); err != nil {
		panic(err)
	}
	if err := k.Fills.Walk(ctx, nil, func(_ uint64, f types.Fill) (bool, error) {
		gs.Fills = append(gs.Fills, f)
		return false, nil
	}); err != nil {
		panic(err)
	}
	gs.NextFill = uint64(len(gs.Fills)) + 1
	if c, ok, err := k.PolygonConfig(ctx); err != nil {
		panic(err)
	} else if ok {
		gs.Polygon = &c
	}
	bz, err := json.Marshal(gs)
	if err != nil {
		panic(err)
	}
	return bz
}

type msgServer struct{ k Keeper }

func (s msgServer) PlaceOffer(ctx context.Context, msg *types.MsgPlaceOffer) (*types.MsgPlaceOfferResponse, error) {
	maker, err := s.k.addressCodec.StringToBytes(msg.Maker)
	if err != nil {
		return nil, err
	}
	id, err := s.k.PlaceOffer(ctx, maker, msg.Side, msg.Lock, msg.Usdc, msg.Polygon, msg.Hash, msg.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &types.MsgPlaceOfferResponse{Id: id}, nil
}

func (s msgServer) CancelOffer(ctx context.Context, msg *types.MsgCancelOffer) (*types.MsgCancelOfferResponse, error) {
	maker, err := s.k.addressCodec.StringToBytes(msg.Maker)
	if err != nil {
		return nil, err
	}
	if err := s.k.CancelOffer(ctx, maker, msg.Id); err != nil {
		return nil, err
	}
	return &types.MsgCancelOfferResponse{}, nil
}

func (s msgServer) TakeOffer(ctx context.Context, msg *types.MsgTakeOffer) (*types.MsgTakeOfferResponse, error) {
	taker, err := s.k.addressCodec.StringToBytes(msg.Taker)
	if err != nil {
		return nil, err
	}
	id, err := s.k.TakeOffer(ctx, taker, msg.Id, msg.Polygon, msg.Hash)
	if err != nil {
		return nil, err
	}
	return &types.MsgTakeOfferResponse{TradeId: id}, nil
}

type queryServer struct{ k Keeper }

func (s queryServer) Offers(ctx context.Context, _ *types.QueryOffersRequest) (*types.QueryOffersResponse, error) {
	offers, err := s.k.AllOffers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Offer, len(offers))
	for i := range offers {
		out[i] = &offers[i]
	}
	return &types.QueryOffersResponse{Offers: out}, nil
}

func (s queryServer) Trades(ctx context.Context, req *types.QueryTradesRequest) (*types.QueryTradesResponse, error) {
	trades, err := s.k.TradesOf(ctx, req.Address)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Trade, len(trades))
	for i := range trades {
		out[i] = &trades[i]
	}
	return &types.QueryTradesResponse{Trades: out}, nil
}

func (s queryServer) Price(ctx context.Context, _ *types.QueryPriceRequest) (*types.QueryPriceResponse, error) {
	last, dayLock, dayUsdc, fills, err := s.k.Price(ctx, 100)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Fill, len(fills))
	for i := range fills {
		out[i] = &fills[i]
	}
	median, err := s.k.DayMedian(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryPriceResponse{Last: last, DayLock: dayLock, DayUsdc: dayUsdc, Recent: out, DayMedian: median}, nil
}

func (s msgServer) ApproveOffer(ctx context.Context, msg *types.MsgApproveOffer) (*types.MsgApproveOfferResponse, error) {
	device, err := s.k.addressCodec.StringToBytes(msg.Device)
	if err != nil {
		return nil, err
	}
	if err := s.k.ApproveOffer(ctx, device, msg.Id); err != nil {
		return nil, err
	}
	return &types.MsgApproveOfferResponse{}, nil
}

func (s queryServer) Polygon(ctx context.Context, _ *types.QueryPolygonRequest) (*types.QueryPolygonResponse, error) {
	c, ok, err := s.k.PolygonConfig(ctx)
	if err != nil || !ok {
		return &types.QueryPolygonResponse{}, err
	}
	return &types.QueryPolygonResponse{ChainId: c.ChainID, Vault: c.Vault, Usdc: c.USDC}, nil
}

func (s queryServer) Candles(ctx context.Context, req *types.QueryCandlesRequest) (*types.QueryCandlesResponse, error) {
	candles, err := s.k.Candles(ctx, req.Interval, int(req.Limit), req.Before)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Candle, len(candles))
	for i := range candles {
		out[i] = &candles[i]
	}
	return &types.QueryCandlesResponse{Candles: out}, nil
}
