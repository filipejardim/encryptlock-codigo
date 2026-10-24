package wallet

import (
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"

	"encryptlock/lockkeeper/protocol"
)

// ParseLOCK converts "2", "0.5" or "0,5" LOCK into lockunit. At most 8
// decimals; must be positive.
func ParseLOCK(s string) (uint64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if len(frac) > protocol.Decimals {
		return 0, fmt.Errorf("more than %d decimals", protocol.Decimals)
	}
	frac += strings.Repeat("0", protocol.Decimals-len(frac))
	// Leading zeros removed: "050000000" would otherwise be read as octal.
	digits := strings.TrimLeft(whole+frac, "0")
	if digits == "" {
		return 0, fmt.Errorf("amount must be greater than zero")
	}
	units, ok := sdkmath.NewIntFromString(digits)
	if !ok || !units.IsPositive() || !units.IsUint64() {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	return units.Uint64(), nil
}

// FormatLOCK shows lockunit as LOCK, without trailing zeros ("2", "0.0015").
func FormatLOCK(units uint64) string {
	whole := units / protocol.BaseUnitsPerCoin
	frac := units % protocol.BaseUnitsPerCoin
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%08d", whole, frac), "0")
}
