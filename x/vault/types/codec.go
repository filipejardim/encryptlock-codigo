package types

import (
	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// ModuleName is the vault module name, store key and the module account that
// holds locked coins.
const ModuleName = "vault"

var (
	ErrInvalidLock = errors.Register(ModuleName, 2, "invalid lock: at least 0,01 LOCK, at most 100 locks per account, the date in the future (at most 10 years)")
)

// RegisterLegacyAminoCodec registers the messages for amino JSON signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgLock{}, "encryptlock/vault/MsgLock", nil)
}

// RegisterInterfaces registers the module's messages.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgLock{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
