package abi

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"golang.org/x/exp/slices"
)

const (
	FtsoCommit    string = "commit"
	FtsoReveal    string = "revealBitvote"
	FtsoSignature string = "signResult"
	FtsoFinalize  string = "finalize"

	FtsoOffers string = "offerRewards"
)

var (
	VotingAbi            abi.ABI
	rewardAbi            abi.ABI
	FtsoVoting           = []string{FtsoCommit, FtsoReveal, FtsoSignature, FtsoFinalize}
	FtsoRewards          = []string{FtsoOffers}
	FtsoPrefixToFuncCall = make(map[string]string)
)

// InitVotingAbi reads the voting contracts, initiates ABI objects
// and creates a correspondence between functions and their ABI codes.
func InitVotingAbi(votingContractFile, rewardContractFile string) {
	file, err := os.ReadFile(votingContractFile)
	if err != nil {
		fmt.Println("Voting contract error", err)
		os.Exit(1)
	}
	var objMap map[string]json.RawMessage
	err = json.Unmarshal(file, &objMap)
	if err != nil {
		fmt.Println("Voting contract error", err)
		os.Exit(1)
	}
	abiBytes, err := objMap["abi"].MarshalJSON()
	if err != nil {
		fmt.Println("Voting contract error", err)
		os.Exit(1)
	}
	r := strings.NewReader(string(abiBytes))
	VotingAbi, err = abi.JSON(r)
	if err != nil {
		fmt.Println("Voting contract error", err)
		os.Exit(1)
	}

	file, err = os.ReadFile(rewardContractFile)
	if err != nil {
		fmt.Println("Reward contract error", err)
		os.Exit(1)
	}
	err = json.Unmarshal(file, &objMap)
	if err != nil {
		fmt.Println("Reward contract error", err)
		os.Exit(1)
	}
	abiBytes, err = objMap["abi"].MarshalJSON()
	if err != nil {
		fmt.Println("Reward contract error", err)
		os.Exit(1)
	}
	r2 := strings.NewReader(string(abiBytes))
	rewardAbi, err = abi.JSON(r2)
	if err != nil {
		fmt.Println("Voting contract error", err)
		os.Exit(1)
	}

	for _, name := range FtsoVoting {
		prefix, err := AbiPrefix(name)
		if err != nil {
			fmt.Println("Voting contract method error", err)
			os.Exit(1)
		}
		FtsoPrefixToFuncCall[prefix] = name
	}

	for _, name := range FtsoRewards {
		prefix, err := AbiPrefix(name)
		if err != nil {
			fmt.Println("Rewards contract method error", err)
			os.Exit(1)
		}
		FtsoPrefixToFuncCall[prefix] = name
	}
}

func MethodByName(name string) (abi.Method, error) {
	if slices.Contains(FtsoVoting, name) {
		return VotingAbi.Methods[name], nil
	} else if slices.Contains(FtsoRewards, name) {
		return rewardAbi.Methods[name], nil
	}
	return abi.Method{}, fmt.Errorf("not a method name")
}

func AbiPrefix(name string) (string, error) {
	method, err := MethodByName(name)
	if err != nil {
		return "", nil
	}

	return hex.EncodeToString(method.ID), nil
}

func DecodeTxParams(data []byte) (map[string]interface{}, error) {
	m, err := MethodByName(FtsoPrefixToFuncCall[hex.EncodeToString(data[:4])])
	if err != nil {
		return map[string]interface{}{}, err
	}
	v := make(map[string]interface{})
	if err := m.Inputs.UnpackIntoMap(v, data[4:]); err != nil {
		return map[string]interface{}{}, err
	}

	return v, nil
}

func EpochFromTimeInt(time uint64, firstEpochStartSec, epochDurationSec int) uint64 {
	return ((time - uint64(firstEpochStartSec)) / uint64(epochDurationSec))
}
