package market

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"encryptlock/lockkeeper/x/market/types"
)

// fakeShield stands in for x/transfers' account protection.
type fakeShield struct {
	frozen, vault map[string]bool
	device        map[string]sdk.AccAddress
}

var errVault = errors.New("vault cannot sell")

func (f fakeShield) CheckSell(_ context.Context, seller sdk.AccAddress, _ uint64) (bool, error) {
	if f.frozen[string(seller)] {
		return false, errors.New("frozen")
	}
	if f.vault[string(seller)] {
		return false, errVault
	}
	return f.device[string(seller)] != nil, nil
}

func (f fakeShield) IsFrozen(_ context.Context, a sdk.AccAddress) (bool, error) {
	return f.frozen[string(a)], nil
}

func (f fakeShield) IsDeviceOf(_ context.Context, owner, signer sdk.AccAddress) (bool, error) {
	d := f.device[string(owner)]
	return d != nil && d.Equals(signer), nil
}

// A thief with the 24 words sells everything to himself: the second device
// must approve, "Not me" stops it, and a vault cannot sell at all.
func TestSellingFollowsTheAccountProtection(t *testing.T) {
	k, bank, ctx := setup(t)
	phone2 := sdk.AccAddress("phone2______________")
	shield := fakeShield{frozen: map[string]bool{}, vault: map[string]bool{}, device: map[string]sdk.AccAddress{string(seller): phone2}}
	k.SetShield(shield)

	id, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 1_000*lockUnit, usdcUnit, sellerPolygon, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, h := secret(1)
	if _, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h); !errors.Is(err, types.ErrAwaitingApproval) {
		t.Fatalf("taking before the second device approves: %v", err)
	}
	if err := k.ApproveOffer(ctx, buyer, id); !errors.Is(err, types.ErrNotDevice) {
		t.Fatalf("approval by someone else: %v", err)
	}
	if err := k.ApproveOffer(ctx, phone2, id); err != nil {
		t.Fatal(err)
	}
	if _, err := k.TakeOffer(ctx, buyer, id, buyerPolygon, h); err != nil {
		t.Fatalf("taking after approval: %v", err)
	}

	// With a second device, taking someone's buy offer (selling) is refused.
	_, bh := secret(2)
	buyID, err := k.PlaceOffer(ctx, buyer, types.Side_SIDE_BUY, lockUnit, usdcUnit, buyerPolygon, bh, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.TakeOffer(ctx, seller, buyID, sellerPolygon, nil); !errors.Is(err, types.ErrNeedsApproval) {
		t.Fatalf("selling into a buy offer with a second device: %v", err)
	}

	// "Not me": open offers come down (LOCK back), nothing more can be sold
	// or bought from the account.
	delete(shield.device, string(seller))
	open, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, 500*lockUnit, usdcUnit, sellerPolygon, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	before := bank.balances[string(seller)]
	shield.frozen[string(seller)] = true
	if err := k.TakeDownOffers(ctx, seller); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Offers.Get(ctx, open); err == nil {
		t.Fatal("open offer still there after Not me")
	}
	if bank.balances[string(seller)] <= before {
		t.Fatal("the LOCK of the open offer did not come back")
	}
	if _, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, sellerPolygon, nil, 0); err == nil {
		t.Fatal("a frozen account placed a sell offer")
	}

	// A Sovereign Vault cannot sell directly.
	shield.frozen[string(seller)] = false
	shield.vault[string(seller)] = true
	if _, err := k.PlaceOffer(ctx, seller, types.Side_SIDE_SELL, lockUnit, usdcUnit, sellerPolygon, nil, 0); !errors.Is(err, errVault) {
		t.Fatalf("a vault placed a sell offer: %v", err)
	}
}
