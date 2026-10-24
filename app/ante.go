package app

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	markettypes "encryptlock/lockkeeper/x/market/types"
	recoverytypes "encryptlock/lockkeeper/x/recovery/types"
	registrytypes "encryptlock/lockkeeper/x/registry/types"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
	vaulttypes "encryptlock/lockkeeper/x/vault/types"
)

// allowedMsgs lists every message the network accepts. Anything else, in
// particular the Cosmos bank MsgSend/MsgMultiSend, is rejected: moving coins
// between people must go through the pending-transfer rules (accept within
// 1 hour or automatic refund), which a direct send would bypass.
var allowedMsgs = map[string]bool{
	// LockKeepers: put coins in guarantee, manage the validator.
	sdk.MsgTypeURL(&stakingtypes.MsgCreateValidator{}):           true,
	sdk.MsgTypeURL(&stakingtypes.MsgEditValidator{}):             true,
	sdk.MsgTypeURL(&stakingtypes.MsgDelegate{}):                  true,
	sdk.MsgTypeURL(&stakingtypes.MsgUndelegate{}):                true,
	sdk.MsgTypeURL(&stakingtypes.MsgBeginRedelegate{}):           true,
	sdk.MsgTypeURL(&stakingtypes.MsgCancelUnbondingDelegation{}): true,
	sdk.MsgTypeURL(&stakingtypes.MsgRotateConsPubKey{}):          true,
	sdk.MsgTypeURL(&distrtypes.MsgWithdrawDelegatorReward{}):     true,
	sdk.MsgTypeURL(&distrtypes.MsgWithdrawValidatorCommission{}): true,
	sdk.MsgTypeURL(&slashingtypes.MsgUnjail{}):                   true,
	sdk.MsgTypeURL(&evidencetypes.MsgSubmitEvidence{}):           true,

	// Accounts: register to be able to receive transfers (free).
	sdk.MsgTypeURL(&registrytypes.MsgRegister{}): true,

	// Transfers between accounts: always pending until accepted (1 hour).
	sdk.MsgTypeURL(&transferstypes.MsgSend{}):   true,
	sdk.MsgTypeURL(&transferstypes.MsgAccept{}): true,
	sdk.MsgTypeURL(&transferstypes.MsgCancel{}): true,

	// Security shield: second device, limits, "Not me", approvals.
	sdk.MsgTypeURL(&transferstypes.MsgSetDevice{}): true,
	sdk.MsgTypeURL(&transferstypes.MsgSetLimits{}): true,
	sdk.MsgTypeURL(&transferstypes.MsgFreeze{}):    true,
	sdk.MsgTypeURL(&transferstypes.MsgUnfreeze{}):  true,
	sdk.MsgTypeURL(&transferstypes.MsgApprove{}):   true,
	sdk.MsgTypeURL(&transferstypes.MsgSetVault{}):  true,

	// Inheritance and recovery by trusted people.
	sdk.MsgTypeURL(&recoverytypes.MsgSetPlan{}):          true,
	sdk.MsgTypeURL(&recoverytypes.MsgAlive{}):            true,
	sdk.MsgTypeURL(&recoverytypes.MsgClaimInheritance{}): true,
	sdk.MsgTypeURL(&recoverytypes.MsgApproveRecovery{}):  true,
	sdk.MsgTypeURL(&recoverytypes.MsgCancelClaims{}):     true,

	// Sovereign Vault: time locks.
	sdk.MsgTypeURL(&vaulttypes.MsgLock{}): true,

	// Market: LOCK ↔ USDC offers (the LOCK are only paid out after the USDC
	// payment is seen on Polygon).
	sdk.MsgTypeURL(&markettypes.MsgPlaceOffer{}):   true,
	sdk.MsgTypeURL(&markettypes.MsgCancelOffer{}):  true,
	sdk.MsgTypeURL(&markettypes.MsgTakeOffer{}):    true,
	sdk.MsgTypeURL(&markettypes.MsgApproveOffer{}): true,

	// LockKeepers: "I am on" every 5 minutes (everyone switched on earns).
	sdk.MsgTypeURL(&rotationtypes.MsgHeartbeat{}):   true,
	sdk.MsgTypeURL(&rotationtypes.MsgSetHelper{}):   true,
	sdk.MsgTypeURL(&rotationtypes.MsgAnnounceKey{}): true,
	sdk.MsgTypeURL(&rotationtypes.MsgProveChip{}):   true,
}

// AllowedMsgsDecorator rejects any transaction carrying a message that is not
// in allowedMsgs.
type AllowedMsgsDecorator struct{}

func (AllowedMsgsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if typeURL := sdk.MsgTypeURL(msg); !allowedMsgs[typeURL] {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "message %s is not allowed on EncryptLock", typeURL)
		}
	}
	return next(ctx, tx, simulate)
}

// IsMsgAllowed reports whether the network accepts a message type.
func IsMsgAllowed(msg sdk.Msg) bool {
	return allowedMsgs[sdk.MsgTypeURL(msg)]
}
