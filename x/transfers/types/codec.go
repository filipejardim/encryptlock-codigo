package types

import (
	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// ModuleName is the transfers module name, store key and the module account
// that holds pending amounts.
const ModuleName = "transfers"

var (
	ErrRecipientNotRegistered = errors.Register(ModuleName, 2, "recipient account is not registered")
	ErrSelfTransfer           = errors.Register(ModuleName, 3, "sender and recipient must differ")
	ErrZeroAmount             = errors.Register(ModuleName, 4, "amount must be greater than zero")
	ErrTransferNotFound       = errors.Register(ModuleName, 5, "pending transfer not found")
	ErrNotRecipient           = errors.Register(ModuleName, 6, "only the recipient can accept this transfer")
	ErrNotSender              = errors.Register(ModuleName, 7, "only the sender can cancel this transfer")
	ErrExpired                = errors.Register(ModuleName, 8, "the 1 hour acceptance window has ended; the amount returns to the sender")
	ErrFrozen                 = errors.Register(ModuleName, 9, "this account is frozen (\"Not me\"): it cannot send")
	ErrDailyLimit             = errors.Register(ModuleName, 10, "this transfer goes over the daily limit")
	ErrAwaitingApproval       = errors.Register(ModuleName, 11, "this transfer is waiting for the sender's second device to approve it")
	ErrNotDevice              = errors.Register(ModuleName, 12, "only the account or its second device can do this")
	ErrNotAwaiting            = errors.Register(ModuleName, 13, "this transfer does not need approval")
	ErrNotFrozen              = errors.Register(ModuleName, 14, "this account is not frozen")
	ErrInvalidDevice          = errors.Register(ModuleName, 15, "the second device must be another account")
	ErrNotReleased            = errors.Register(ModuleName, 16, "this transfer comes from a Sovereign Vault and cannot be accepted yet")
	ErrInvalidVault           = errors.Register(ModuleName, 17, "a vault delay must be between 7 and 365 days, or 0")
	ErrVaultCannotSell        = errors.Register(ModuleName, 18, "a Sovereign Vault cannot sell directly: send the LOCK to a normal account first (with the vault delay)")
)

// RegisterLegacyAminoCodec registers the messages for amino JSON signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSend{}, "encryptlock/transfers/MsgSend", nil)
	cdc.RegisterConcrete(&MsgAccept{}, "encryptlock/transfers/MsgAccept", nil)
	cdc.RegisterConcrete(&MsgCancel{}, "encryptlock/transfers/MsgCancel", nil)
	cdc.RegisterConcrete(&MsgSetDevice{}, "encryptlock/transfers/MsgSetDevice", nil)
	cdc.RegisterConcrete(&MsgSetLimits{}, "encryptlock/transfers/MsgSetLimits", nil)
	cdc.RegisterConcrete(&MsgFreeze{}, "encryptlock/transfers/MsgFreeze", nil)
	cdc.RegisterConcrete(&MsgUnfreeze{}, "encryptlock/transfers/MsgUnfreeze", nil)
	cdc.RegisterConcrete(&MsgApprove{}, "encryptlock/transfers/MsgApprove", nil)
	cdc.RegisterConcrete(&MsgSetVault{}, "encryptlock/transfers/MsgSetVault", nil)
}

// RegisterInterfaces registers the module's messages.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgSend{}, &MsgAccept{}, &MsgCancel{},
		&MsgSetDevice{}, &MsgSetLimits{}, &MsgFreeze{}, &MsgUnfreeze{}, &MsgApprove{}, &MsgSetVault{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
