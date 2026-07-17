package contracts

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAddressesForUpgradedContract(t *testing.T) {
	oldAddress := common.HexToAddress("0x2580101692366e2f331e891180d9ffdF861Fce83")
	newAddress := common.HexToAddress("0xA480457953Af3583E54DCd630b219353B8FC9Af7")

	require.Equal(t, []common.Address{oldAddress, newAddress}, addressesForUpgradedContract("VoterRegistry", oldAddress))
	require.Equal(t, []common.Address{oldAddress, newAddress}, addressesForUpgradedContract("VoterRegistry", newAddress))

	unrelated := common.HexToAddress("0x0000000000000000000000000000000000000001")
	require.Equal(t, []common.Address{unrelated}, addressesForUpgradedContract("VoterRegistry", unrelated))
	require.Equal(t, []common.Address{oldAddress}, addressesForUpgradedContract("OtherContract", oldAddress))
}
