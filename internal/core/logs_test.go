package core

import (
	"testing"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"

	"github.com/stretchr/testify/require"
)

func TestValidateCollectLogs(t *testing.T) {
	valid := []config.LogInfo{
		{ContractAddress: "0x1c78A073E3BD2aCa4cc327d55FB0cD4f0549B55b", Topic: "undefined"},
		{ContractAddress: "1c78A073E3BD2aCa4cc327d55FB0cD4f0549B55b", Topic: ""},
		{ContractAddress: "undefined", Topic: "0x91d0280e969157fc6c5b8f952f237b03d934b18534dafcac839075bbc33522f8"},
		{ContractAddress: "", Topic: "91d0280e969157fc6c5b8f952f237b03d934b18534dafcac839075bbc33522f8"},
	}
	require.NoError(t, validateCollectLogs(valid))

	invalid := []struct {
		name string
		info config.LogInfo
	}{
		{"address not hex", config.LogInfo{ContractAddress: "0xNOTHEX", Topic: "undefined"}},
		{"address too short", config.LogInfo{ContractAddress: "0x1c78A073", Topic: "undefined"}},
		{"topic not hex", config.LogInfo{ContractAddress: "undefined", Topic: "0xZZ"}},
		{"topic too short", config.LogInfo{ContractAddress: "undefined", Topic: "0x91d0280e"}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, validateCollectLogs([]config.LogInfo{tc.info}))
		})
	}
}

func TestParseLogFilters(t *testing.T) {
	addrs, err := parseLogAddresses("0x1c78A073E3BD2aCa4cc327d55FB0cD4f0549B55b")
	require.NoError(t, err)
	require.Len(t, addrs, 1)

	addrs, err = parseLogAddresses("undefined")
	require.NoError(t, err)
	require.Nil(t, addrs, "undefined means no address filter")

	topics, err := parseLogTopics("0x91d0280e969157fc6c5b8f952f237b03d934b18534dafcac839075bbc33522f8")
	require.NoError(t, err)
	require.Len(t, topics, 1)
	require.Len(t, topics[0], 1)

	topics, err = parseLogTopics("")
	require.NoError(t, err)
	require.Nil(t, topics, "empty means no topic filter")
}
