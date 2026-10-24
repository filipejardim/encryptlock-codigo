package app

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"encryptlock/lockkeeper/hybrid"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
)

// accountGetter is the part of the account keeper HybridSignerDecorator uses.
type accountGetter interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// HybridSignerDecorator rejects any transaction signed by an account whose key
// is not hybrid (Ed25519 + ML-DSA-65, both required). It must run after
// SetPubKeyDecorator, which records the key of a new account.
type HybridSignerDecorator struct {
	ak accountGetter
}

func (d HybridSignerDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	for _, signer := range signers {
		acc := d.ak.GetAccount(ctx, signer)
		if acc == nil {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrUnknownAddress, "account %s does not exist", sdk.AccAddress(signer))
		}
		pk := acc.GetPubKey()
		// Gas simulation uses placeholder keys; real transactions never skip.
		if simulate && (pk == nil || !hybrid.IsAllowed(pk)) {
			continue
		}
		if pk == nil || !hybrid.IsAllowed(pk) {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey,
				"account %s must use a hybrid key (Ed25519 + ML-DSA-65, 2 of 2)", sdk.AccAddress(signer))
		}
	}
	return next(ctx, tx, simulate)
}

// newAnteHandler is the Cosmos SDK default ante chain (x/auth/ante.NewAnteHandler)
// with EncryptLock's rules inserted: allowed messages first, hybrid keys
// right after the account key is set and before signatures are checked, and
// the 1-dollar minimum guarantee for a new LockKeeper.
//
// A registration by a brand-new account takes a shorter chain: no fee (it is
// free), and NewAccountRegistrationDecorator in place of the account-based
// signature checks.
func newAnteHandler(options ante.HandlerOptions, ak registrationAccountKeeper, activity ActivityRecorder, market referencePricer, fair fairUse, closed closedAccounts) sdk.AnteHandler {
	registration := sdk.ChainAnteDecorators(
		ante.NewSetUpContextDecorator(),
		AllowedMsgsDecorator{},
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		NewAccountRegistrationDecorator{ak: ak, signModeHandler: options.SignModeHandler, sigGasConsumer: options.SigGasConsumer, fair: fair},
	)
	normal := sdk.ChainAnteDecorators(
		ante.NewSetUpContextDecorator(),
		AllowedMsgsDecorator{},
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		ClosedAccountDecorator{closed: closed},
		ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker),
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		HybridSignerDecorator{ak: options.AccountKeeper},
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler, options.SigVerifyOptions...),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		MinGuaranteeDecorator{market: market},
		FairUseDecorator{fair: fair},
		ActivityDecorator{recorder: activity},
	)

	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if isNewAccountRegistration(ctx, options.AccountKeeper, tx) {
			return registration(ctx, tx, simulate)
		}
		return normal(ctx, tx, simulate)
	}
}

// closedAccounts tells whether an account was already inherited or recovered
// (x/recovery forwards).
type closedAccounts interface {
	IsClosed(ctx context.Context, addr sdk.AccAddress) (bool, error)
}

// ClosedAccountDecorator rejects any transaction signed by an account that was
// already inherited or recovered. Its coins belong to the heirs or the new
// account: whoever still has its keys (a thief, after a recovery) must not
// stop the guarantee from coming back, send the LockKeeper's earnings
// elsewhere or use the account in any other way.
type ClosedAccountDecorator struct{ closed closedAccounts }

func (d ClosedAccountDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	for _, s := range signers {
		closed, err := d.closed.IsClosed(ctx, s)
		if err != nil {
			return ctx, err
		}
		if closed {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"account %s was inherited or recovered: its coins now go to the heirs or the new account", sdk.AccAddress(s))
		}
	}
	return next(ctx, tx, simulate)
}

// fairUse is the registry's fair-use counter (x/registry/fairuse.go).
type fairUse interface {
	CountTx(ctx context.Context, addr sdk.AccAddress, paid bool) error
	CountRegistration(ctx context.Context) error
}

// FairUseDecorator counts each signer's transactions this hour; past the
// limit they wait for the next hour. A transaction with a transfer pays its
// fee and does not count. It runs after the signatures are checked.
type FairUseDecorator struct {
	fair fairUse
}

func (d FairUseDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !simulate {
		paid := false
		for _, msg := range tx.GetMsgs() {
			if _, ok := msg.(*transferstypes.MsgSend); ok {
				paid = true
			}
		}
		sigTx, ok := tx.(authsigning.SigVerifiableTx)
		if !ok {
			return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
		}
		signers, err := sigTx.GetSigners()
		if err != nil {
			return ctx, err
		}
		for _, signer := range signers {
			if err := d.fair.CountTx(ctx, signer, paid); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

// ActivityRecorder records a sign of life (inheritance, white paper 7.3).
type ActivityRecorder interface {
	RecordActivity(ctx context.Context, addr sdk.AccAddress) error
}

// ActivityDecorator records every signer of a transaction as alive. It runs
// after the signatures are checked, so only the real owner counts.
type ActivityDecorator struct {
	recorder ActivityRecorder
}

func (d ActivityDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !simulate {
		sigTx, ok := tx.(authsigning.SigVerifiableTx)
		if !ok {
			return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
		}
		signers, err := sigTx.GetSigners()
		if err != nil {
			return ctx, err
		}
		for _, signer := range signers {
			if err := d.recorder.RecordActivity(ctx, signer); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}
