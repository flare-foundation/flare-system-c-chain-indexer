package indexer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/abi"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type TransactionsBatch struct {
	Transactions []*types.Transaction
	toBlock      []*types.Block
	toReceipt    []*types.Receipt
	sync.Mutex
}

func NewTransactionsBatch() *TransactionsBatch {
	transactionBatch := TransactionsBatch{}
	transactionBatch.Transactions = make([]*types.Transaction, 0)
	transactionBatch.toBlock = make([]*types.Block, 0)
	transactionBatch.toReceipt = make([]*types.Receipt, 0)

	return &transactionBatch
}

func countReceipts(txs *TransactionsBatch) int {
	i := 0
	for _, e := range txs.toReceipt {
		if e != nil {
			i++
		}
	}

	return i
}

func (ci *BlockIndexer) getTransactionsReceipt(transactionBatch *TransactionsBatch,
	start, stop int, errChan chan error) {
	var receipt *types.Receipt
	var err error
	for i := start; i < stop; i++ {
		tx := transactionBatch.Transactions[i]
		txData := hex.EncodeToString(tx.Data())
		funcSig := txData[:8]
		contractAddress := strings.ToLower(tx.To().Hex()[2:])
		if ci.transactions[contractAddress][funcSig][0] || ci.transactions[contractAddress][funcSig][1] {
			for j := 0; j < config.ReqRepeats; j++ {
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
				receipt, err = ci.client.TransactionReceipt(ctx, tx.Hash())
				cancelFunc()
				if err == nil {
					break
				}
			}
			if err != nil {
				errChan <- fmt.Errorf("getTransactionsReceipt: %w", err)
				return
			}
		} else {
			receipt = nil
		}

		transactionBatch.toReceipt[i] = receipt
	}

	errChan <- nil
}

func (ci *BlockIndexer) processTransactions(transactionBatch *TransactionsBatch) (*DatabaseStructData, error) {
	data := NewDatabaseStructData()
	for i, tx := range transactionBatch.Transactions {
		block := transactionBatch.toBlock[i]
		txData := hex.EncodeToString(tx.Data())
		funcSig := txData[:8]
		contractAddress := strings.ToLower(tx.To().Hex()[2:])
		fromAddress, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx) // todo: this is a bit slow
		if err != nil {
			return nil, fmt.Errorf("processTransactions: Sender: %w", err)
		}
		epoch := abi.EpochFromTimeInt(block.Time(), ci.epochParams.FirstEpochStartSec, ci.epochParams.EpochDurationSec)
		status := uint64(2)
		if transactionBatch.toReceipt[i] != nil {
			status = transactionBatch.toReceipt[i].Status
			// if it was chosen to get the logs of the transaction we process it
			if ci.transactions[contractAddress][funcSig][1] {
				log, err := json.Marshal(transactionBatch.toReceipt[i].Logs)
				if err != nil {
					return nil, fmt.Errorf("processTransactions: Marshall: %w", err)
				}
				dbLog := &database.FtsoLog{
					TxHash:    tx.Hash().Hex()[2:],
					Log:       string(log),
					Timestamp: block.Time(),
				}
				data.Logs = append(data.Logs, dbLog)
			}
		}

		dbTx := &database.FtsoTransaction{
			Hash:      tx.Hash().Hex()[2:],
			Data:      txData,
			BlockId:   block.NumberU64(),
			FuncSig:   funcSig,
			Status:    status,
			From:      fromAddress.Hex()[2:],
			To:        tx.To().Hex()[2:],
			Timestamp: block.Time(),
		}
		data.Transactions = append(data.Transactions, dbTx)

		// if the option to create a specific table is chosen, we process the transaction and extract info
		funcName, ok := abi.FtsoPrefixToFuncCall[funcSig]
		if !ok {
			continue
		}
		if _, ok := ci.optTables[funcName]; !ok {
			continue
		}
		parametersMap, err := abi.DecodeTxParams(tx.Data())
		if err != nil {
			// todo: to be removed, just for songbird benchmark
			if err.Error() == "not a method name" {
				continue
			}
			return nil, fmt.Errorf("processTransactions: %w", err)
		}

		switch funcName {
		case abi.FtsoCommit:
			commit, err := processCommit(parametersMap, fromAddress, epoch, block.Time(), tx.Hash().Hex()[2:])
			if err != nil {
				return nil, fmt.Errorf("processTransactions: %w", err)
			}

			data.Commits = append(data.Commits, commit)
		case abi.FtsoReveal:
			reveal, err := processReveal(parametersMap, fromAddress, epoch, block.Time(), tx.Hash().Hex()[2:])
			if err != nil {
				return nil, fmt.Errorf("processTransactions: %w", err)
			}
			data.Reveals = append(data.Reveals, reveal)
		case abi.FtsoSignature:
			signatureData, err := processSignature(parametersMap, fromAddress, block.Time(), epoch, tx.Hash().Hex()[2:])
			if err != nil {
				return nil, fmt.Errorf("processTransactions: %w", err)
			}
			data.Signatures = append(data.Signatures, signatureData)
		case abi.FtsoFinalize:
			finalization, err := processFinalization(parametersMap, fromAddress, block.Time(), epoch, tx.Hash().Hex()[2:])
			if err != nil {
				return nil, fmt.Errorf("processTransactions: %w", err)
			}
			data.Finalizations = append(data.Finalizations, finalization)
		case abi.FtsoOffers:
			offers, err := processRewardOffers(parametersMap, fromAddress, block.Time(), epoch, tx.Hash().Hex()[2:])
			if err != nil {
				return nil, fmt.Errorf("processTransactions: %w", err)
			}
			data.RewardOffers = append(data.RewardOffers, offers...)
		}
	}

	return data, nil
}

func processCommit(parametersMap map[string]interface{}, fromAddress common.Address,
	epoch uint64, timestamp uint64, hash string) (*database.Commit, error) {
	commitHashInterface, ok := parametersMap["_commitHash"]
	if ok == false {
		return nil, fmt.Errorf("processCommit: input commitHash not found")
	}
	commitHash, ok := commitHashInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("processCommit: input commitHash not correctly formed")
	}
	commit := &database.Commit{
		Epoch:      epoch,
		Address:    fromAddress.Hex()[2:],
		CommitHash: hex.EncodeToString(commitHash[:]),
		Timestamp:  timestamp,
		TxHash:     hash,
	}

	return commit, nil
}

func processReveal(parametersMap map[string]interface{}, fromAddress common.Address,
	epoch uint64, timestamp uint64, hash string) (*database.Reveal, error) {
	randomInterface, ok := parametersMap["_random"]
	if ok == false {
		return nil, fmt.Errorf("processReveal: input random not found")
	}
	random, ok := randomInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("processReveal: input random not correctly formed")
	}
	merkleRootInterface, ok := parametersMap["_merkleRoot"]
	if ok == false {
		return nil, fmt.Errorf("processReveal: input merkleRoot not found")
	}
	merkleRoot, ok := merkleRootInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("processReveal: input merkleRoot not correctly formed")
	}

	bitVoteInterface, ok := parametersMap["_bitVote"]
	if ok == false {
		return nil, fmt.Errorf("processReveal: input bitVote not found")
	}
	bitVote, ok := bitVoteInterface.([]byte)
	if ok == false {
		return nil, fmt.Errorf("processReveal: input bitVote not correctly formed")
	}

	pricesInterface, ok := parametersMap["_prices"]
	if ok == false {
		return nil, fmt.Errorf("processReveal: input prices not found")
	}
	prices, ok := pricesInterface.([]byte)
	if ok == false {
		return nil, fmt.Errorf("processReveal: input prices not correctly formed")
	}

	reveal := &database.Reveal{
		Epoch:      epoch,
		Address:    fromAddress.Hex()[2:],
		Random:     hex.EncodeToString(random[:]),
		MerkleRoot: hex.EncodeToString(merkleRoot[:]),
		BitVote:    hex.EncodeToString(bitVote),
		Prices:     hex.EncodeToString(prices),
		Timestamp:  timestamp,
		TxHash:     hash,
	}

	return reveal, nil
}

func processSignature(parametersMap map[string]interface{}, fromAddress common.Address,
	timestamp uint64, blockEpoch uint64, hash string) (*database.SignatureData, error) {
	epochInterface, ok := parametersMap["_epochId"]
	if ok == false {
		return nil, fmt.Errorf("processSignature: input epoch not found")
	}
	epoch, ok := epochInterface.(*big.Int)
	if ok == false {
		return nil, fmt.Errorf("processSignature: input epoch not correctly formed")
	}

	merkleRootInterface, ok := parametersMap["_merkleRoot"]
	if ok == false {
		return nil, fmt.Errorf("processSignature: input merkleRoot not found")
	}
	merkleRoot, ok := merkleRootInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("processSignature: input merkleRoot not correctly formed")
	}

	signatureInterface, ok := parametersMap["signature"]
	if ok == false {
		return nil, fmt.Errorf("processSignature: input signature not found")
	}
	signature, err := json.Marshal(signatureInterface)
	if err != nil {
		return nil, fmt.Errorf("processSignature: input signature not correctly formed %s", err)
	}

	signatureData := &database.SignatureData{
		Epoch:          blockEpoch,
		SignatureEpoch: epoch.Uint64(),
		Address:        fromAddress.Hex()[2:],
		MerkleRoot:     hex.EncodeToString(merkleRoot[:]),
		Signature:      string(signature),
		Timestamp:      timestamp,
		TxHash:         hash,
	}

	return signatureData, nil
}

func processFinalization(parametersMap map[string]interface{}, fromAddress common.Address,
	timestamp uint64, blockEpoch uint64, hash string) (*database.Finalization, error) {
	epochInterface, ok := parametersMap["_epochId"]
	if ok == false {
		return nil, fmt.Errorf("processFinalization: input epoch not found")
	}
	epoch, ok := epochInterface.(*big.Int)
	if ok == false {
		return nil, fmt.Errorf("processFinalization: input epoch not correctly formed")
	}

	merkleRootInterface, ok := parametersMap["_merkleRoot"]
	if ok == false {
		return nil, fmt.Errorf("processFinalization: input merkleRoot not found")
	}
	merkleRoot, ok := merkleRootInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("processFinalization: input merkleRoot not correctly formed")
	}

	signaturesInterface, ok := parametersMap["signatures"]
	if ok == false {
		return nil, fmt.Errorf("processFinalization: input signature not found")
	}
	signatures, err := json.Marshal(signaturesInterface)
	if err != nil {
		return nil, fmt.Errorf("processFinalization: input signatures not correctly formed %s", err)
	}

	finalization := &database.Finalization{
		Epoch:          blockEpoch,
		SignatureEpoch: epoch.Uint64(),
		Address:        fromAddress.Hex()[2:],
		MerkleRoot:     hex.EncodeToString(merkleRoot[:]),
		Signatures:     string(signatures),
		Timestamp:      timestamp,
		TxHash:         hash,
	}

	return finalization, nil
}

func processRewardOffers(parametersMap map[string]interface{}, fromAddress common.Address,
	timestamp uint64, blockEpoch uint64, hash string) ([]*database.RewardOffer, error) {
	offersInterface, ok := parametersMap["offers"]
	if ok == false {
		return nil, fmt.Errorf("processRewardOffers: input offers not found")
	}
	// type gymnastics
	offersBytes, err := json.Marshal(offersInterface)
	if err != nil {
		return nil, fmt.Errorf("processRewardOffers: input offers not correctly formed %s", err)
	}
	var offers []abi.Offer
	err = json.Unmarshal(offersBytes, &offers)
	if err != nil {
		return nil, fmt.Errorf("processRewardOffers: %w", err)
	}

	rewardOffers := make([]*database.RewardOffer, len(offers))
	for i, offer := range offers {
		leadProvidersHex := make([]string, len(offer.LeadProviders))
		for j, provider := range offer.LeadProviders {
			leadProvidersHex[j] = provider.Hex()[2:]
		}
		providers, err := json.Marshal(leadProvidersHex)
		if err != nil {
			return nil, fmt.Errorf("processRewardOffers: %w", err)
		}

		rewardOffers[i] = &database.RewardOffer{
			Epoch:               blockEpoch,
			Address:             fromAddress.Hex()[2:],
			Amount:              offer.Amount.Uint64(),
			CurrencyAddress:     offer.CurrencyAddress.Hex()[2:],
			OfferSymbol:         hex.EncodeToString(offer.OfferSymbol[:]),
			QuoteSymbol:         hex.EncodeToString(offer.QuoteSymbol[:]),
			LeadProviders:       string(providers),
			RewardBeltPPM:       offer.RewardBeltPPM.Uint64(),
			ElasticBandWidthPPM: offer.RewardBeltPPM.Uint64(),
			IqrSharePPM:         offer.IqrSharePPM.Uint64(),
			PctSharePPM:         offer.PctSharePPM.Uint64(),
			RemainderClaimer:    offer.RemainderClaimer.Hex()[2:],
			Timestamp:           timestamp,
			TxHash:              hash,
		}
	}

	return rewardOffers, nil
}
