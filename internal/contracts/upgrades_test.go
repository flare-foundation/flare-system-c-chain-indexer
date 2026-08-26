package contracts

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAddressesForUpgradedContract(t *testing.T) {
	oldAddress := common.HexToAddress("0xCcF30790A93F15e24EB909548a2C58a9b0a7FBd4")
	newAddress := common.HexToAddress("0x5A2Eb0cdB4Aa8253924a488A77EdfD24Bb64407f")

	require.Equal(t, []common.Address{oldAddress, newAddress}, addressesForUpgradedContract("Relay", oldAddress))
	require.Equal(t, []common.Address{oldAddress, newAddress}, addressesForUpgradedContract("Relay", newAddress))

	unrelated := common.HexToAddress("0x0000000000000000000000000000000000000001")
	require.Equal(t, []common.Address{unrelated}, addressesForUpgradedContract("Relay", unrelated))
	require.Equal(t, []common.Address{oldAddress}, addressesForUpgradedContract("OtherContract", oldAddress))
}

func TestRelayAddressesAcrossTheCutover(t *testing.T) {
	relays := map[string][2]common.Address{}
	for _, contract := range fspUpgradedContracts {
		if contract.name == "Relay" {
			relays[contract.old.Hex()] = [2]common.Address{contract.old, contract.new}
		}
	}
	require.Len(t, relays, 4, "one Relay pair per network")

	for _, pair := range relays {
		old, current := pair[0], pair[1]
		require.NotEqual(t, old, current)
		expected := []common.Address{old, current}
		require.Equal(t, expected, addressesForUpgradedContract("Relay", old))
		require.Equal(t, expected, addressesForUpgradedContract("Relay", current))
	}
}
