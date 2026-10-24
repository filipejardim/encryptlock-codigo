package types

import (
	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// ModuleName is the rotation module name, store key and the account that
// holds the earnings until they are shared out.
const ModuleName = "rotation"

var (
	ErrNotLockKeeper = errors.Register(ModuleName, 2, "account is not a LockKeeper")
	ErrTooSoon       = errors.Register(ModuleName, 3, "heartbeat sent too soon")
	ErrHelperTaken   = errors.Register(ModuleName, 4, "helper cannot be used")
	ErrBadKey        = errors.Register(ModuleName, 5, "not a block-signing key")
	ErrNoChip        = errors.Register(ModuleName, 6, "no real computer: the security chip of this LockKeeper is not proven")
	ErrBadChip       = errors.Register(ModuleName, 7, "chip ticket not accepted")
)

// RegisterLegacyAminoCodec registers the messages for amino JSON signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgHeartbeat{}, "encryptlock/rotation/MsgHeartbeat", nil)
	cdc.RegisterConcrete(&MsgSetHelper{}, "encryptlock/rotation/MsgSetHelper", nil)
	cdc.RegisterConcrete(&MsgAnnounceKey{}, "encryptlock/rotation/MsgAnnounceKey", nil)
	cdc.RegisterConcrete(&MsgProveChip{}, "encryptlock/rotation/MsgProveChip", nil)
}

// RegisterInterfaces registers the module's messages.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgHeartbeat{}, &MsgSetHelper{}, &MsgAnnounceKey{}, &MsgProveChip{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
