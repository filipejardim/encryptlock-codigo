package app

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	errorsmod "cosmossdk.io/errors"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"encryptlock/lockkeeper/hybrid"
	registrytypes "encryptlock/lockkeeper/x/registry/types"
)

// isNewAccountRegistration reports whether tx is a registration by an account
// the network does not know yet (it never received coins). Such an account
// has no account number, so the normal Cosmos signature check cannot run.
func isNewAccountRegistration(ctx sdk.Context, ak ante.AccountKeeper, tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}
	msg, ok := msgs[0].(*registrytypes.MsgRegister)
	if !ok {
		return false
	}
	addr, err := ak.AddressCodec().StringToBytes(msg.Address)
	if err != nil {
		return false
	}
	return ak.GetAccount(ctx, addr) == nil
}

// registrationAccountKeeper is the account keeper plus account creation.
type registrationAccountKeeper interface {
	ante.AccountKeeper
	NewAccountWithAddress(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// NewAccountRegistrationDecorator verifies the hybrid signatures of a new
// account's registration (with account number 0 and sequence 0, the values a
// wallet can know before the account exists), then creates the account.
//
// Replaying the same transaction later fails: the account then exists, so the
// transaction goes through the normal checks, where the account number and
// sequence no longer match.
type NewAccountRegistrationDecorator struct {
	ak              registrationAccountKeeper
	signModeHandler *txsigning.HandlerMap
	sigGasConsumer  ante.SignatureVerificationGasConsumer
	fair            fairUse
}

func (d NewAccountRegistrationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}
	if !sigTx.GetFee().IsZero() {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "account registration is free: the fee must be 0")
	}
	if utx, ok := tx.(sdk.TxWithUnordered); ok && utx.GetUnordered() {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "account registration cannot be unordered")
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	pubKeys, err := sigTx.GetPubKeys()
	if err != nil {
		return ctx, err
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}
	if len(signers) != 1 || len(pubKeys) != 1 || len(sigs) != 1 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "registration must have exactly one signer")
	}

	signer, pk, sig := sdk.AccAddress(signers[0]), pubKeys[0], sigs[0]
	if pk == nil || !hybrid.IsAllowed(pk) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "account %s must use a hybrid key (Ed25519 + ML-DSA-65, 2 of 2)", signer)
	}
	if !signer.Equals(sdk.AccAddress(pk.Address())) {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "public key does not match the registered address")
	}
	if sig.Sequence != 0 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrWrongSequence, "a new account registers with sequence 0")
	}

	if err := d.sigGasConsumer(ctx.GasMeter(), sig, d.ak.GetParams(ctx)); err != nil {
		return ctx, err
	}

	if !simulate {
		anyPk, err := codectypes.NewAnyWithValue(pk)
		if err != nil {
			return ctx, err
		}
		signerData := txsigning.SignerData{
			Address:       signer.String(),
			ChainID:       ctx.ChainID(),
			AccountNumber: 0,
			Sequence:      0,
			PubKey:        &anypb.Any{TypeUrl: anyPk.TypeUrl, Value: anyPk.Value},
		}
		adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
		if !ok {
			return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
		}
		if err := authsigning.VerifySignature(ctx, pk, signerData, sig.Data, d.signModeHandler, adaptableTx.GetSigningTxData()); err != nil {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"registration signature verification failed; sign with account number 0, sequence 0 and chain-id %s", ctx.ChainID())
		}
	}

	// At most a few new accounts per block (fair use).
	if d.fair != nil {
		if err := d.fair.CountRegistration(ctx); err != nil {
			return ctx, err
		}
	}

	// Create the account with its hybrid key; sequence 1 = this tx is used.
	acc := d.ak.NewAccountWithAddress(ctx, signer)
	if err := acc.SetPubKey(pk); err != nil {
		return ctx, err
	}
	if err := acc.SetSequence(1); err != nil {
		return ctx, err
	}
	d.ak.SetAccount(ctx, acc)

	return next(ctx, tx, simulate)
}
