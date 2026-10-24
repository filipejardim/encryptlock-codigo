package app

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// When old blocks were made (22 Oct 2026). A LockKeeper keeps whole blocks
// for only about 40 minutes, and the pace of blocks changed (1,4 s, then 5 s):
// an estimate from today's pace put a transfer of two days ago a week back.
// Before deleting them, the LockKeeper writes down the time of one block in
// every BlockTimesEvery (cmd/lockkeeper/cmd/diet.go); the history places an
// older transfer between the two nearest notes (a few minutes off at most).

// BlockTimesFile is the notes file, in the LockKeeper's data folder: one
// "height,unix seconds" line per note, in order of height.
const BlockTimesFile = "block-times.csv"

// BlockTimesEvery: one note per this many blocks (~3 KB a day).
const BlockTimesEvery = 100

// AppendBlockTime adds a note (only the LockKeeper's clean-up writes them).
func AppendBlockTime(path string, height int64, t time.Time) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%d,%d\n", height, t.Unix()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type blockTime struct {
	height int64
	at     time.Time
}

// blockTimes reads the notes again when the file grew.
type blockTimes struct {
	path string
	mu   sync.Mutex
	size int64
	list []blockTime
}

func (b *blockTimes) notes() []blockTime {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, err := os.Stat(b.path)
	if err != nil || st.Size() == b.size {
		return b.list
	}
	f, err := os.Open(b.path)
	if err != nil {
		return b.list
	}
	defer f.Close()
	var list []blockTime
	s := bufio.NewScanner(f)
	for s.Scan() {
		h, t, ok := strings.Cut(s.Text(), ",")
		height, err1 := strconv.ParseInt(h, 10, 64)
		unix, err2 := strconv.ParseInt(t, 10, 64)
		if ok && err1 == nil && err2 == nil {
			list = append(list, blockTime{height, time.Unix(unix, 0).UTC()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].height < list[j].height })
	b.list, b.size = list, st.Size()
	return list
}

// estimateTime places a block between the nearest notes (the last note can
// be the oldest block still kept); false when there are none around it.
func estimateTime(notes []blockTime, height int64) (time.Time, bool) {
	i := sort.Search(len(notes), func(i int) bool { return notes[i].height >= height })
	switch {
	case i < len(notes) && notes[i].height == height:
		return notes[i].at, true
	case i == 0 || i == len(notes):
		return time.Time{}, false
	}
	a, b := notes[i-1], notes[i]
	part := float64(height-a.height) / float64(b.height-a.height)
	return a.at.Add(time.Duration(part * float64(b.at.Sub(a.at)))), true
}
