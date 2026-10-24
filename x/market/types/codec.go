package types

import (
	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// ModuleName is the market module name, store key and the module account that
// holds the LOCK of offers and trades.
const ModuleName = "market"

var (
	ErrInvalidOffer     = errors.Register(ModuleName, 2, "invalid offer: amounts must be positive, the 0x address valid and the date within a year")
	ErrInvalidHash      = errors.Register(ModuleName, 3, "the hash of the secret must have 32 bytes")
	ErrNotFound         = errors.Register(ModuleName, 4, "offer or trade not found")
	ErrNotMaker         = errors.Register(ModuleName, 5, "only who placed the offer can cancel it")
	ErrReserved         = errors.Register(ModuleName, 6, "this offer is reserved for a trade in progress")
	ErrOwnOffer         = errors.Register(ModuleName, 7, "you cannot take your own offer")
	ErrNotRegistered    = errors.Register(ModuleName, 8, "the account that receives the LOCK must be registered")
	ErrWrongSecret      = errors.Register(ModuleName, 9, "the secret does not match this trade")
	ErrReservationEnd   = errors.Register(ModuleName, 10, "the reservation of this trade has ended")
	ErrExpired          = errors.Register(ModuleName, 11, "this offer has expired")
	ErrHashUsed         = errors.Register(ModuleName, 12, "this secret is already in use in a trade or was already used")
	ErrNoPolygon        = errors.Register(ModuleName, 13, "this network does not watch USDC payments on Polygon yet: offers cannot be taken")
	ErrTooManyOffers    = errors.Register(ModuleName, 14, "too many open offers from this account")
	ErrTooManyTrades    = errors.Register(ModuleName, 15, "this account already has trades waiting for payment")
	ErrWaitAfterUnpaid  = errors.Register(ModuleName, 16, "a trade of this account was not paid: wait a few hours before taking another offer")
	ErrSmallOffer       = errors.Register(ModuleName, 17, "an offer must be worth at least 1 USDC")
	ErrAwaitingApproval = errors.Register(ModuleName, 18, "this sell offer waits for the seller's second device to approve it")
	ErrNeedsApproval    = errors.Register(ModuleName, 19, "an account with a second device sells with a sell offer, which the second device approves")
	ErrNotDevice        = errors.Register(ModuleName, 20, "only the seller's second device can approve this offer")
	ErrSellerFrozen     = errors.Register(ModuleName, 21, "the seller pressed Not me: this offer cannot be taken")
)

// RegisterLegacyAminoCodec registers the messages for amino JSON signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgPlaceOffer{}, "encryptlock/market/MsgPlaceOffer", nil)
	cdc.RegisterConcrete(&MsgCancelOffer{}, "encryptlock/market/MsgCancelOffer", nil)
	cdc.RegisterConcrete(&MsgTakeOffer{}, "encryptlock/market/MsgTakeOffer", nil)
	cdc.RegisterConcrete(&MsgApproveOffer{}, "encryptlock/market/MsgApproveOffer", nil)
}

// RegisterInterfaces registers the module's messages.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgPlaceOffer{}, &MsgCancelOffer{}, &MsgTakeOffer{}, &MsgApproveOffer{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
