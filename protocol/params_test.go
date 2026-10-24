package protocol

import (
	"testing"
	"time"
)

func TestProtocolParameters(t *testing.T) {
	if Decimals != 8 {
		t.Errorf("Decimals = %d, want 8", Decimals)
	}
	if MaxSupplyBaseUnits != 4_000_000_000_000_000 {
		t.Errorf("MaxSupplyBaseUnits = %d, want 4000000000000000", MaxSupplyBaseUnits)
	}
	if IssuanceMonths != 360 {
		t.Errorf("IssuanceMonths = %d, want 360", IssuanceMonths)
	}
	if HalvingPeriodMonths != 60 {
		t.Errorf("HalvingPeriodMonths = %d, want 60", HalvingPeriodMonths)
	}
	if TransferAcceptanceWindow != time.Hour {
		t.Errorf("TransferAcceptanceWindow = %s, want 1h", TransferAcceptanceWindow)
	}
}
