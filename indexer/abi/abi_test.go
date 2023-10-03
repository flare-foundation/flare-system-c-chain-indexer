package abi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAbi(t *testing.T) {
	InitVotingAbi("contracts/Voting.json", "contracts/VotingRewardManager.json")

	prefix, err := AbiPrefix(FtsoCommit)
	assert.NoError(t, err)
	assert.Equal(t, "f14fcbc8", prefix)

	prefix, err = AbiPrefix(FtsoFinalize)
	assert.NoError(t, err)
	assert.NotEmpty(t, prefix)

	prefix, err = AbiPrefix(FtsoOffers)
	assert.NoError(t, err)
	assert.NotEmpty(t, prefix)

	prefix, err = AbiPrefix(FtsoReveal)
	assert.NoError(t, err)
	assert.NotEmpty(t, prefix)

	prefix, err = AbiPrefix(FtsoSignature)
	assert.NoError(t, err)
	assert.NotEmpty(t, prefix)
}
