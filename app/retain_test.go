package app

import "testing"

func TestRetainHeightKeepsOnlyTheLastBlocks(t *testing.T) {
	for _, c := range []struct{ sdk, height, keep, want int64 }{
		{140_000, 240_000, 480, 239_520}, // the SDK's 100.000 evidence blocks: raised
		{0, 50_000, 480, 49_520},         // young chain, the SDK keeps everything: raised
		{0, 300, 480, 0},                 // fewer blocks than kept: nothing deleted
		{239_800, 240_000, 480, 239_800}, // the SDK already deletes more: kept
	} {
		if got := retainHeight(c.sdk, c.height, c.keep); got != c.want {
			t.Errorf("retainHeight(%d, %d, %d) = %d, want %d", c.sdk, c.height, c.keep, got, c.want)
		}
	}
}
