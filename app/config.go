package app

import (
	clienthelpers "cosmossdk.io/client/v2/helpers"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"encryptlock/lockkeeper/protocol"
)

const (
	AppName = "LockKeeper"

	// Bech32Prefix gives addresses like lock1q...
	Bech32Prefix = protocol.Bech32Prefix

	// BaseDenom is the smallest unit: 1 LOCK = 100.000.000 lockunit.
	BaseDenom    = "lockunit"
	DisplayDenom = "LOCK"

	// RulesAuthorityName names the address set as "authority" on every module.
	// No module account, key or governance exists behind it, so nobody can
	// ever sign as it: module parameters cannot be changed on-chain.
	RulesAuthorityName = "encryptlock-golden-rules"
)

// DefaultNodeHome is where a LockKeeper keeps its data (~/.lockkeeper).
var DefaultNodeHome string

func init() {
	var err error
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory(".lockkeeper")
	if err != nil {
		panic(err)
	}

	sdk.DefaultBondDenom = BaseDenom
	// One unit of voting power per whole LOCK in guarantee.
	sdk.DefaultPowerReduction = sdkmath.NewIntFromUint64(protocol.BaseUnitsPerCoin)
}

// RulesAuthority returns the unreachable authority address (see RulesAuthorityName).
func RulesAuthority() string {
	return authtypes.NewModuleAddress(RulesAuthorityName).String()
}

// SetAddressPrefixes configures lock1... addresses. Call once, before anything
// else uses addresses.
func SetAddressPrefixes() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(Bech32Prefix, Bech32Prefix+"pub")
	cfg.SetBech32PrefixForValidator(Bech32Prefix+"valoper", Bech32Prefix+"valoperpub")
	cfg.SetBech32PrefixForConsensusNode(Bech32Prefix+"valcons", Bech32Prefix+"valconspub")
}
