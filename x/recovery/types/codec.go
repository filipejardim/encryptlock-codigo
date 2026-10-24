package types

import (
	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// ModuleName is the recovery module name and store key.
const ModuleName = "recovery"

var (
	ErrInvalidPlan     = errors.Register(ModuleName, 2, "invalid plan")
	ErrNoPlan          = errors.Register(ModuleName, 3, "this account has no heirs or trusted people")
	ErrNotHeir         = errors.Register(ModuleName, 4, "you are not an heir of this account")
	ErrNotGuardian     = errors.Register(ModuleName, 5, "you are not a trusted person of this account")
	ErrStillActive     = errors.Register(ModuleName, 6, "this account showed a sign of life too recently")
	ErrClaimRunning    = errors.Register(ModuleName, 7, "a claim is already running for this account")
	ErrNotRegistered   = errors.Register(ModuleName, 8, "the account must be registered")
	ErrNothingToCancel = errors.Register(ModuleName, 9, "there is nothing to cancel")
	ErrClosed          = errors.Register(ModuleName, 10, "this account was already inherited or recovered")
	ErrNoLivingHeir    = errors.Register(ModuleName, 11, "nobody can receive the coins: every heir was already inherited or recovered")
)

// RegisterLegacyAminoCodec registers the messages for amino JSON signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSetPlan{}, "encryptlock/recovery/MsgSetPlan", nil)
	cdc.RegisterConcrete(&MsgAlive{}, "encryptlock/recovery/MsgAlive", nil)
	cdc.RegisterConcrete(&MsgClaimInheritance{}, "encryptlock/recovery/MsgClaimInheritance", nil)
	cdc.RegisterConcrete(&MsgApproveRecovery{}, "encryptlock/recovery/MsgApproveRecovery", nil)
	cdc.RegisterConcrete(&MsgCancelClaims{}, "encryptlock/recovery/MsgCancelClaims", nil)
}

// RegisterInterfaces registers the module's messages.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetPlan{}, &MsgAlive{}, &MsgClaimInheritance{}, &MsgApproveRecovery{}, &MsgCancelClaims{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
