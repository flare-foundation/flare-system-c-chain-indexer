package fsp

import (
	"context"
	"flare-ftso-indexer/internal/config"
	"flare-ftso-indexer/internal/contracts"
	"flare-ftso-indexer/internal/policylog"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const undefinedTopic = "undefined"

func resolveFspContractAddresses(
	ctx context.Context,
	resolver *contracts.ContractResolver,
) ([]common.Address, []common.Hash, error) {
	defaultFspLogs := config.FspRewardEpochLogs()
	logAddresses := make([]common.Address, 0, len(defaultFspLogs))
	logTopics := make([]common.Hash, 0, len(defaultFspLogs))
	seenAddresses := make(map[common.Address]struct{}, len(defaultFspLogs))
	seenTopics := make(map[common.Hash]struct{}, len(defaultFspLogs))

	for _, logInfo := range defaultFspLogs {
		contractName := strings.TrimSpace(logInfo.ContractName)
		if contractName == "" {
			continue
		}

		address, err := resolver.ResolveByName(ctx, contractName)
		if err != nil {
			return nil, nil, err
		}

		if _, ok := seenAddresses[address]; !ok {
			seenAddresses[address] = struct{}{}
			logAddresses = append(logAddresses, address)
		}

		topic := strings.TrimSpace(logInfo.Topic)
		if topic == "" || strings.EqualFold(topic, undefinedTopic) {
			continue
		}
		if !strings.HasPrefix(topic, "0x") {
			topic = "0x" + topic
		}

		topicHash := common.HexToHash(strings.ToLower(topic))
		if _, ok := seenTopics[topicHash]; ok {
			continue
		}
		seenTopics[topicHash] = struct{}{}
		logTopics = append(logTopics, topicHash)
	}

	policylog.LogFspEventFilter(defaultFspLogs)

	return logAddresses, logTopics, nil
}
