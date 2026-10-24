package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestBlockTimesPlaceOldBlocksBetweenNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), BlockTimesFile)
	start := time.Date(2026, 10, 18, 17, 50, 55, 0, time.UTC)
	// 1,4 s per block up to 1000, then 5 s: one line straight from today's
	// pace would put block 500 days off.
	for _, n := range []struct {
		h int64
		t time.Time
	}{{1, start}, {500, start.Add(700 * time.Second)}, {1000, start.Add(1400 * time.Second)}, {1100, start.Add(1900 * time.Second)}} {
		if err := AppendBlockTime(path, n.h, n.t); err != nil {
			t.Fatal(err)
		}
	}
	b := &blockTimes{path: path}
	notes := b.notes()
	if len(notes) != 4 {
		t.Fatalf("%d notes", len(notes))
	}
	for _, c := range []struct {
		h    int64
		want time.Time
	}{
		{500, start.Add(700 * time.Second)},   // a note
		{250, start.Add(349 * time.Second)},   // between 1 and 500 (1,4 s per block)
		{1050, start.Add(1650 * time.Second)}, // between 1000 and 1100 (5 s per block)
	} {
		got, ok := estimateTime(notes, c.h)
		if !ok || got.Sub(c.want).Abs() > 2*time.Second {
			t.Errorf("block %d at %v (%v), want %v", c.h, got, ok, c.want)
		}
	}
	if _, ok := estimateTime(notes, 2000); ok {
		t.Error("after the last note: nothing to place it between")
	}
	// New notes are read when the file grows.
	_ = AppendBlockTime(path, 1200, start.Add(2400*time.Second))
	if got, ok := estimateTime(b.notes(), 1150); !ok || !got.Equal(start.Add(2150*time.Second)) {
		t.Errorf("after a new note: %v %v", got, ok)
	}
}

// A transfer whose block is gone gets its time from the notes, not from
// today's pace of blocks.
func TestHistoryUsesTheNotesForDeletedBlocks(t *testing.T) {
	me := sdk.AccAddress(strings.Repeat("m", 20)).String()
	other := sdk.AccAddress(strings.Repeat("o", 20)).String()
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	f := fakeIndex{start: start, txs: map[string][]*coretypes.ResultTx{
		"transfer_accepted.recipient='" + me + "'": {accepted(400, "3", other, me, "50")},
	}}
	notes := []blockTime{{300, start.Add(-10 * time.Hour)}, {500, start.Add(-8 * time.Hour)}}
	got, err := history(context.Background(), f, notes, me, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if want := start.Add(-9 * time.Hour); !got[0].Estimated || !got[0].Time.Equal(want) {
		t.Errorf("time %v (estimated %v), want %v", got[0].Time, got[0].Estimated, want)
	}
}
