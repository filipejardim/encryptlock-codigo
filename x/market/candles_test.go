package market

import (
	"testing"

	"encryptlock/lockkeeper/x/market/types"
)

func TestCandlesFromTrades(t *testing.T) {
	k, _, ctx := setup(t)
	// 10 LOCK = 1e9 units; 2 USDC = 2e6 → 0,20 $ per LOCK = 200.000.000 billionths.
	fills := []types.Fill{
		{TradeId: 1, Lock: 1e9, Usdc: 2e6, Time: 600},  // 0,20 · minute 600
		{TradeId: 2, Lock: 1e9, Usdc: 5e6, Time: 630},  // 0,50
		{TradeId: 3, Lock: 1e9, Usdc: 1e6, Time: 640},  // 0,10
		{TradeId: 4, Lock: 1e9, Usdc: 3e6, Time: 659},  // 0,30 (close)
		{TradeId: 5, Lock: 2e9, Usdc: 8e6, Time: 1260}, // 0,40 · minute 1260
	}
	for i, f := range fills {
		if err := k.Fills.Set(ctx, uint64(i+1), f); err != nil {
			t.Fatal(err)
		}
	}
	c, err := k.Candles(ctx, 60, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 2 {
		t.Fatalf("want 2 candles, got %d", len(c))
	}
	want := types.Candle{Time: 600, Open: 2e8, High: 5e8, Low: 1e8, Close: 3e8, Lock: 4e9, Usdc: 11e6}
	if c[0] != want {
		t.Fatalf("first candle %+v, want %+v", c[0], want)
	}
	if c[1].Time != 1260 || c[1].Open != 4e8 || c[1].Close != 4e8 {
		t.Fatalf("second candle %+v", c[1])
	}
	// One candle per hour: everything together.
	h, _ := k.Candles(ctx, 3600, 10, 0)
	if len(h) != 1 || h[0].Open != 2e8 || h[0].Close != 4e8 || h[0].High != 5e8 || h[0].Low != 1e8 {
		t.Fatalf("hour candle %+v", h)
	}
	// Only the newest candle when asked for one.
	last, _ := k.Candles(ctx, 60, 1, 0)
	if len(last) != 1 || last[0].Time != 1260 {
		t.Fatalf("limit 1: %+v", last)
	}
	if _, err := k.Candles(ctx, 10, 10, 0); err == nil {
		t.Fatal("an interval under a minute must be refused")
	}
}

// The whole history, page by page: candles before a time, never a candle cut
// in two, and nothing before the first trade.
func TestCandlesPageByPage(t *testing.T) {
	k, _, ctx := setup(t)
	// One trade per minute for 1.000 minutes, price = its minute number.
	const n = 1000
	for i := 1; i <= n; i++ {
		f := types.Fill{TradeId: uint64(i), Lock: 1e8, Usdc: uint64(i) * 1e4, Time: int64(i) * 60}
		if err := k.Fills.Set(ctx, uint64(i), f); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.NextFill.Set(ctx, n+1); err != nil {
		t.Fatal(err)
	}
	var all []types.Candle
	before := int64(0)
	for page := 0; page < 10; page++ {
		c, err := k.Candles(ctx, 60, 300, before)
		if err != nil {
			t.Fatal(err)
		}
		if len(c) == 0 {
			break
		}
		all = append(c, all...)
		before = c[0].Time
	}
	if len(all) != n || all[0].Time != 60 || all[n-1].Time != n*60 {
		t.Fatalf("%d candles from %d to %d, want %d from 60", len(all), all[0].Time, all[len(all)-1].Time, n)
	}
	for i := 1; i < n; i++ {
		if all[i].Time != all[i-1].Time+60 {
			t.Fatalf("gap or repeat at %d", all[i].Time)
		}
	}
	// A time inside a candle: that candle is not given (it is not whole).
	c, _ := k.Candles(ctx, 600, 500, 1250)
	if len(c) == 0 || c[len(c)-1].Time != 600 {
		t.Fatalf("before 1250 in 10-minute candles: last %+v, want the one at 600", c[len(c)-1])
	}
	if c, _ := k.Candles(ctx, 60, 10, 60); len(c) != 0 {
		t.Fatalf("before the first trade: %d candles", len(c))
	}
}
