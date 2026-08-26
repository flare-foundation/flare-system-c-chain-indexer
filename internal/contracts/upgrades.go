package contracts

import "github.com/ethereum/go-ethereum/common"

type upgradedContract struct {
	name string
	old  common.Address
	new  common.Address
}

// fspUpgradedContracts keeps both deployments of a redeployed contract indexed.
var fspUpgradedContracts = []upgradedContract{
	// Relay
	{"Relay", common.HexToAddress("0xCcF30790A93F15e24EB909548a2C58a9b0a7FBd4"), common.HexToAddress("0x5A2Eb0cdB4Aa8253924a488A77EdfD24Bb64407f")}, // Flare
	{"Relay", common.HexToAddress("0xCB86E8Be709001e01897Bf59847406853da8f14b"), common.HexToAddress("0xc1BC89b717Af42AE27497C9FFb996002D3AC5031")}, // Songbird
	{"Relay", common.HexToAddress("0x051f214D346Cfd97B107BECb87E2B35D1b4287E9"), common.HexToAddress("0xEcD0B60Ea5E01e4D0bFd621c8920B40A32389b83")}, // Coston
	{"Relay", common.HexToAddress("0xa10B672D1c62e5457b17af63d4302add6A99d7dE"), common.HexToAddress("0x5017728F117501A24EF9C3756C07f0d564598596")}, // Coston2
}

func addressesForUpgradedContract(contractName string, current common.Address) []common.Address {
	for _, contract := range fspUpgradedContracts {
		if contract.name != contractName {
			continue
		}
		if current == contract.old || current == contract.new {
			return []common.Address{contract.old, contract.new}
		}
	}

	return []common.Address{current}
}
