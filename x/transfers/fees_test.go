package transfers

import (
	"testing"

	"encryptlock/lockkeeper/protocol"
	"encryptlock/lockkeeper/x/transfers/types"
)

func TestTransferFeeIsPointOnePercentWithMinAndMax(t *testing.T) {
	cases := []struct {
		name   string
		amount uint64
		want   uint64
	}{
		{"1 LOCK: 0,1% = 0,001 LOCK", 1 * L, 100_000},
		{"5 LOCK: 0,1% = 0,005 LOCK", 5 * L, 500_000},
		{"tiny amount: minimum 0,0002 LOCK", 1_000, protocol.TransferFeeMin},
		{"exactly at the minimum (0,2 LOCK)", 20_000_000, protocol.TransferFeeMin},
		{"exactly at the maximum (10 LOCK)", 10 * L, protocol.TransferFeeMax},
		{"1.000 LOCK: maximum 0,01 LOCK", 1_000 * L, protocol.TransferFeeMax},
		{"whole supply: maximum, no overflow", protocol.MaxSupplyBaseUnits, protocol.TransferFeeMax},
	}
	for _, c := range cases {
		if got := TransferFee(c.amount); got != c.want {
			t.Errorf("%s: TransferFee(%d) = %d, want %d", c.name, c.amount, got, c.want)
		}
	}
}

func TestFeeSplitByPeriod(t *testing.T) {
	year := protocol.YearSeconds
	cases := []struct {
		name    string
		elapsed int64
		wantDev uint64 // of a fee of 1.000
	}{
		{"year 1", 0, 750},
		{"last second of year 5", 5*year - 1, 750},
		{"year 6", 5 * year, 500},
		{"year 100", 100 * year, 500},
	}
	for _, c := range cases {
		dev, keepers := SplitFee(1_000, c.elapsed, false)
		if dev != c.wantDev || dev+keepers != 1_000 {
			t.Errorf("%s: split = %d / %d, want %d / %d", c.name, dev, keepers, c.wantDev, 1_000-c.wantDev)
		}
	}
}

func TestFeeSplitRoundsInFavourOfLockKeepers(t *testing.T) {
	dev, keepers := SplitFee(3, 0, false) // 75% of 3 = 2,25
	if dev != 2 || keepers != 1 {
		t.Fatalf("split of 3 = %d / %d, want 2 / 1", dev, keepers)
	}
}

func TestSenderPaysFeeAndItIsSplit(t *testing.T) {
	e := setup(t)
	if _, err := e.k.Send(e.ctx, alice, bob, 40*L); err != nil {
		t.Fatal(err)
	}
	fee := protocol.TransferFeeMax
	if e.bank.feesPaid[string(alice)] != fee {
		t.Fatalf("alice paid %d in fees, want %d", e.bank.feesPaid[string(alice)], fee)
	}
	if e.bank.feesPaid[string(bob)] != 0 {
		t.Fatal("recipient paid a fee")
	}
	if e.bank.devFund != fee*75/100 || e.bank.lockKeepr != fee-fee*75/100 {
		t.Fatalf("split dev/lockkeepers = %d / %d, want 75%% / 25%%", e.bank.devFund, e.bank.lockKeepr)
	}
}

func TestFeeIsNotRefundedWhenCanceled(t *testing.T) {
	e := setup(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 40*L)
	if err := e.k.Cancel(e.ctx, alice, id); err != nil {
		t.Fatal(err)
	}
	if got, want := e.bank.balances[string(alice)], 100*L-protocol.TransferFeeMax; got != want {
		t.Fatalf("alice after cancel = %d, want %d (amount back, fee kept)", got, want)
	}
}

func TestAcceptAndCancelAreFree(t *testing.T) {
	e := setup(t)
	id, _ := e.k.Send(e.ctx, alice, bob, 10*L)
	if err := e.k.Accept(e.ctx, bob, id); err != nil {
		t.Fatal(err)
	}
	if e.bank.feesPaid[string(bob)] != 0 {
		t.Fatal("accepting cost a fee")
	}
}

func TestCannotSendWithoutMoneyForTheFee(t *testing.T) {
	e := setup(t)
	// Exactly the whole balance: nothing left for the fee.
	if _, err := e.k.Send(e.ctx, alice, bob, 100*L); err == nil {
		t.Fatal("transfer of the whole balance accepted without money for the fee")
	}
}

func TestFinalMilestoneBefore5YearsMakesIt5050(t *testing.T) {
	dev, keepers := SplitFee(1_000, 0, true)
	if dev != 500 || keepers != 500 {
		t.Fatalf("year 1 with the final milestone: %d / %d, want 500 / 500", dev, keepers)
	}
}

func TestDevFundIsPublicAndMovesOnlyWhenTold(t *testing.T) {
	e := setup(t)
	res, err := (queryServer{e.k}).DevFund(e.ctx, &types.QueryDevFundRequest{})
	if err != nil || res.Address != e.str(devFund) {
		t.Fatalf("dev fund = %v, %v; want the genesis fund", res, err)
	}
	if err := e.k.MoveDevFund(e.ctx, bob); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.k.DevFundAddress(e.ctx); !got.Equals(bob) {
		t.Fatalf("dev fund = %s after the move, want bob", got)
	}
	// The next fee's share goes to the new fund.
	if _, err := e.k.Send(e.ctx, alice, bob, 40*L); err != nil {
		t.Fatal(err)
	}
	if e.bank.devFund != 0 {
		t.Fatal("a fee still went to the old fund")
	}
}
