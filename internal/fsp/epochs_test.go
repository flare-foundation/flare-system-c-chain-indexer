package fsp

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/require"
)

type fakeEpoch struct {
	startTs    uint64
	startBlock uint64
	raTs       uint64
	raBlock    uint64
}

// fakeFSM serves epoch data from a map; epochs without an entry read as
// zero-valued, matching the contract's bare storage reads.
type fakeFSM struct {
	current uint64
	epochs  map[uint64]fakeEpoch
}

func (f *fakeFSM) GetCurrentRewardEpochId(_ *bind.CallOpts) (*big.Int, error) {
	return new(big.Int).SetUint64(f.current), nil
}

func (f *fakeFSM) GetRewardEpochStartInfo(_ *bind.CallOpts, rewardEpochID *big.Int) (rewardEpochStartInfo, error) {
	e := f.epochs[rewardEpochID.Uint64()]
	return rewardEpochStartInfo{
		RewardEpochStartTs:    e.startTs,
		RewardEpochStartBlock: e.startBlock,
	}, nil
}

func (f *fakeFSM) GetRandomAcquisitionInfo(_ *bind.CallOpts, rewardEpochID *big.Int) (randomAcquisitionInfo, error) {
	e := f.epochs[rewardEpochID.Uint64()]
	return randomAcquisitionInfo{
		RandomAcquisitionStartTs:    e.raTs,
		RandomAcquisitionStartBlock: e.raBlock,
	}, nil
}

// startedEpochs builds a chain where epochs [first, last] have start data and
// every epoch except first also has random-acquisition data recorded ~2h
// before its start.
func startedEpochs(first, last uint64) map[uint64]fakeEpoch {
	epochs := make(map[uint64]fakeEpoch, last-first+1)
	for e := first; e <= last; e++ {
		epoch := fakeEpoch{
			startTs:    (e + 1) * 1_000_000,
			startBlock: (e + 1) * 10_000,
		}
		if e > first {
			epoch.raTs = epoch.startTs - 7200
			epoch.raBlock = epoch.startBlock - 100
		}
		epochs[e] = epoch
	}
	return epochs
}

func TestHistoryStartEpochID(t *testing.T) {
	tests := []struct {
		name          string
		current       uint64
		historyEpochs uint64
		want          uint64
	}{
		{name: "window within history", current: 100, historyEpochs: 5, want: 96},
		{name: "single epoch", current: 100, historyEpochs: 1, want: 100},
		{name: "zero treated as current epoch", current: 100, historyEpochs: 0, want: 100},
		{name: "window covers whole history", current: 4, historyEpochs: 5, want: 0},
		{name: "window exceeds history", current: 4, historyEpochs: 100, want: 0},
		{name: "epoch zero", current: 0, historyEpochs: 3, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, historyStartEpochID(tc.current, tc.historyEpochs))
		})
	}
}

func TestOldestEpochWithStartInfo(t *testing.T) {
	fsm := &fakeFSM{current: 250, epochs: startedEpochs(223, 250)}

	tests := []struct {
		name   string
		lo, hi uint64
		wantID uint64
		wantOk bool
	}{
		{name: "whole range has data", lo: 240, hi: 250, wantID: 240, wantOk: true},
		{name: "clamps to oldest with data", lo: 100, hi: 250, wantID: 223, wantOk: true},
		{name: "range below data", lo: 0, hi: 222, wantOk: false},
		{name: "single epoch with data", lo: 223, hi: 223, wantID: 223, wantOk: true},
		{name: "single epoch without data", lo: 5, hi: 5, wantOk: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, info, ok, err := oldestEpochWithStartInfo(context.Background(), fsm, tc.lo, tc.hi)
			require.NoError(t, err)
			require.Equal(t, tc.wantOk, ok)
			if ok {
				require.Equal(t, tc.wantID, id)
				require.Equal(t, fsm.epochs[tc.wantID].startBlock, info.RewardEpochStartBlock)
				require.Equal(t, fsm.epochs[tc.wantID].startTs, info.RewardEpochStartTs)
			}
		})
	}
}

func TestOldestEpochWithStartInfoAtEpochZero(t *testing.T) {
	fsm := &fakeFSM{current: 3, epochs: startedEpochs(0, 3)}

	id, _, ok, err := oldestEpochWithStartInfo(context.Background(), fsm, 0, 3)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(0), id)
}
