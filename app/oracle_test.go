package app

import (
	"encoding/json"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
)

func vote(power int64, paid ...paidTrade) abci.ExtendedVoteInfo {
	var ext []byte
	if len(paid) > 0 {
		ext, _ = json.Marshal(paidVote{Paid: paid})
	}
	return abci.ExtendedVoteInfo{Validator: abci.Validator{Power: power}, VoteExtension: ext, BlockIdFlag: cmtproto.BlockIDFlagCommit}
}

func TestOnlyTradesSeenPaidByMoreThanTwoThirdsSettle(t *testing.T) {
	s1, s2 := strings.Repeat("11", 32), strings.Repeat("22", 32)
	a, b, fake := paidTrade{Trade: 1, Secret: s1}, paidTrade{Trade: 2, Secret: s2}, paidTrade{Trade: 2, Secret: s1}
	absent := vote(10, a, b)
	absent.BlockIdFlag = cmtproto.BlockIDFlagAbsent
	commit := abci.ExtendedCommitInfo{Votes: []abci.ExtendedVoteInfo{
		vote(25, a, b, a), // a listed twice counts once
		vote(25, a, b),
		vote(25, a, fake),
		vote(15),
		absent, // did not vote: its power still counts in the total
	}}
	// Total 100: a = 75 (> 66,6) settles; b = 50 does not; fake = 25 does not.
	got := tallyPaid(commit)
	if len(got) != 1 || got[0] != a {
		t.Fatalf("settled %+v, want only trade 1", got)
	}
	if _, err := decodeVote([]byte(`{"paid":[{"t":1,"s":"zz"}]}`)); err == nil {
		t.Error("a vote with a bad secret was accepted")
	}
}
