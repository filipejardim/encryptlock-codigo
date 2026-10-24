package app

import (
	"testing"

	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type fakeTx struct{ msgs []sdk.Msg }

func (t fakeTx) GetMsgs() []sdk.Msg                    { return t.msgs }
func (t fakeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (t fakeTx) ValidateBasic() error                  { return nil }
func (t fakeTx) GetSigners() ([][]byte, error)         { return nil, nil }

func runAllowedMsgs(msgs ...sdk.Msg) (nextCalled bool, err error) {
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	}
	_, err = AllowedMsgsDecorator{}.AnteHandle(sdk.Context{}, fakeTx{msgs: msgs}, false, next)
	return nextCalled, err
}

func TestDirectSendIsRejected(t *testing.T) {
	for _, msg := range []sdk.Msg{&banktypes.MsgSend{}, &banktypes.MsgMultiSend{}} {
		nextCalled, err := runAllowedMsgs(msg)
		if err == nil || nextCalled {
			t.Errorf("%s was accepted; direct sends must be rejected", sdk.MsgTypeURL(msg))
		}
	}
}

func TestDirectSendHiddenAmongAllowedMsgsIsRejected(t *testing.T) {
	nextCalled, err := runAllowedMsgs(&stakingtypes.MsgDelegate{}, &banktypes.MsgSend{})
	if err == nil || nextCalled {
		t.Error("a transaction mixing an allowed message with MsgSend was accepted")
	}
}

func TestStakingMessagesAreAllowed(t *testing.T) {
	nextCalled, err := runAllowedMsgs(&stakingtypes.MsgCreateValidator{}, &stakingtypes.MsgDelegate{})
	if err != nil || !nextCalled {
		t.Errorf("staking messages were rejected: %v", err)
	}
}
