package contracts

import "github.com/ethereum/go-ethereum/common"

type upgradedContract struct {
	name string
	old  common.Address
	new  common.Address
}

// fspUpgradedContracts keeps both deployments of upgraded voter contracts
// indexed.
var fspUpgradedContracts = []upgradedContract{
	// Songbird
	{"VoterPreRegistry", common.HexToAddress("0x9Ba9A142FD5B2953667B03dB40D1d77c83F225a2"), common.HexToAddress("0xD8957603dE539118898BA2C321a1001d062Be7Ae")},
	{"FlareSystemsCalculator", common.HexToAddress("0x126FAeEc75601dA3354c0b5Cc0b60C85fCbC3A5e"), common.HexToAddress("0x31a5B8E7ca6dFC7B963f5D029F0884ef19E53A24")},
	{"VoterRegistry", common.HexToAddress("0x31B9EC65C731c7D973a33Ef3FC83B653f540dC8D"), common.HexToAddress("0xd23FAE88c09e6A77dD9eFcc29D6bBC55D2e74310")},

	// Flare
	{"VoterPreRegistry", common.HexToAddress("0xeFDBf6F31Aa46c62414Aee82aF43036d16885b48"), common.HexToAddress("0x76D49E62B07e52A13b7FBB4602eD942f812c87e2")},
	{"FlareSystemsCalculator", common.HexToAddress("0x67c4B11c710D35a279A41cff5eb089Fe72748CF8"), common.HexToAddress("0xf9cCe0Bd286bb38A9A0cD15fDDC5431F03568Db0")},
	{"VoterRegistry", common.HexToAddress("0x2580101692366e2f331e891180d9ffdF861Fce83"), common.HexToAddress("0xA480457953Af3583E54DCd630b219353B8FC9Af7")},
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
