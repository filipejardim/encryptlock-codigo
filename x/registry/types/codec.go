package types

import (
	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// ModuleName is the registry module name and store key.
const ModuleName = "registry"

var (
	ErrAlreadyRegistered = errors.Register(ModuleName, 2, "account is already registered")
	ErrNotRegistered     = errors.Register(ModuleName, 3, "account is not registered")
)

// RegisterLegacyAminoCodec registers the messages for amino JSON signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegister{}, "encryptlock/registry/MsgRegister", nil)
}

// RegisterInterfaces registers the module's messages.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgRegister{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
