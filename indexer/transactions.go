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
	"golang.org/x/exp/slices"
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
	receiptCheck := strings.Split(ci.params.Receipts, ",")
	for i := start; i < stop; i++ {
		tx := transactionBatch.Transactions[i]
		txData := hex.EncodeToString(tx.Data())
		funcCall := abi.FtsoPrefixToFuncCall[txData[:8]]
		if slices.Contains(receiptCheck, funcCall) || ci.params.Receipts == "all" {
			for j := 0; j < config.ReqRepeats; j++ {
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
				receipt, err = ci.client.TransactionReceipt(ctx, tx.Hash())
				cancelFunc()
				if err == nil {
					break
				}
			}
			if err != nil {
				errChan <- err
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
		funcCall := abi.FtsoPrefixToFuncCall[txData[:8]]

		fromAddress, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil {
			return nil, err
		}
		epoch := abi.EpochFromTimeInt(block.Time(), ci.epochParams.FirstEpochStartSec, ci.epochParams.EpochDurationSec)
		status := uint64(2)
		if transactionBatch.toReceipt[i] != nil {
			status = transactionBatch.toReceipt[i].Status
		}

		dbTx := &database.FtsoTransaction{
			Hash:      tx.Hash().Hex()[2:],
			Data:      txData,
			BlockId:   block.NumberU64(),
			Method:    funcCall,
			Status:    status,
			From:      fromAddress.Hex()[2:],
			To:        tx.To().Hex()[2:],
			Timestamp: block.Time(),
		}
		data.Transactions = append(data.Transactions, dbTx)

		parametersMap, err := abi.DecodeTxParams(tx.Data())
		if err != nil {
			// todo: to be removed, just for songbird benchmark
			if err.Error() == "not a method name" {
				continue
			}
			return nil, err
		}

		// if the option to create a specific table is chosen, we process the transaction and extract info
		if _, ok := ci.optTables[funcCall]; !ok {
			continue
		}
		switch funcCall {
		case abi.FtsoCommit:
			commit, err := processCommit(parametersMap, fromAddress, epoch, block.Time(), tx.Hash().Hex()[2:])
			if err != nil {
				return nil, err
			}

			data.Commits = append(data.Commits, commit)
		case abi.FtsoReveal:
			reveal, err := processReveal(parametersMap, fromAddress, epoch, block.Time(), tx.Hash().Hex()[2:])
			if err != nil {
				return nil, err
			}
			data.Reveals = append(data.Reveals, reveal)
		case abi.FtsoSignature:
			signatureData, err := processSignature(parametersMap, fromAddress, block.Time(), epoch, tx.Hash().Hex()[2:])
			if err != nil {
				return nil, err
			}
			data.Signatures = append(data.Signatures, signatureData)
		case abi.FtsoFinalize:
			finalization, err := processFinalization(parametersMap, fromAddress, block.Time(), epoch, tx.Hash().Hex()[2:])
			if err != nil {
				return nil, err
			}
			data.Finalizations = append(data.Finalizations, finalization)
		case abi.FtsoOffers:
			offers, err := processRewardOffers(parametersMap, fromAddress, block.Time(), epoch, tx.Hash().Hex()[2:])
			if err != nil {
				return nil, err
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
		return nil, fmt.Errorf("input commitHash not found")
	}
	commitHash, ok := commitHashInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("input commitHash not correctly formed")
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
		return nil, fmt.Errorf("input random not found")
	}
	random, ok := randomInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("input random not correctly formed")
	}
	merkleRootInterface, ok := parametersMap["_merkleRoot"]
	if ok == false {
		return nil, fmt.Errorf("input merkleRoot not found")
	}
	merkleRoot, ok := merkleRootInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("input merkleRoot not correctly formed")
	}

	bitVoteInterface, ok := parametersMap["_bitVote"]
	if ok == false {
		return nil, fmt.Errorf("input bitVote not found")
	}
	bitVote, ok := bitVoteInterface.([]byte)
	if ok == false {
		return nil, fmt.Errorf("input bitVote not correctly formed")
	}

	pricesInterface, ok := parametersMap["_prices"]
	if ok == false {
		return nil, fmt.Errorf("input prices not found")
	}
	prices, ok := pricesInterface.([]byte)
	if ok == false {
		return nil, fmt.Errorf("input prices not correctly formed")
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
		return nil, fmt.Errorf("input epoch not found")
	}
	epoch, ok := epochInterface.(*big.Int)
	if ok == false {
		return nil, fmt.Errorf("input epoch not correctly formed")
	}

	merkleRootInterface, ok := parametersMap["_merkleRoot"]
	if ok == false {
		return nil, fmt.Errorf("input merkleRoot not found")
	}
	merkleRoot, ok := merkleRootInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("input merkleRoot not correctly formed")
	}

	signatureInterface, ok := parametersMap["signature"]
	if ok == false {
		return nil, fmt.Errorf("input signature not found")
	}
	signature, err := json.Marshal(signatureInterface)
	if err != nil {
		return nil, fmt.Errorf("input signature not correctly formed %s", err)
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
		return nil, fmt.Errorf("input epoch not found")
	}
	epoch, ok := epochInterface.(*big.Int)
	if ok == false {
		return nil, fmt.Errorf("input epoch not correctly formed")
	}

	merkleRootInterface, ok := parametersMap["_merkleRoot"]
	if ok == false {
		return nil, fmt.Errorf("input merkleRoot not found")
	}
	merkleRoot, ok := merkleRootInterface.([32]byte)
	if ok == false {
		return nil, fmt.Errorf("input merkleRoot not correctly formed")
	}

	signaturesInterface, ok := parametersMap["signatures"]
	if ok == false {
		return nil, fmt.Errorf("input signature not found")
	}
	signatures, err := json.Marshal(signaturesInterface)
	if err != nil {
		return nil, fmt.Errorf("input signatures not correctly formed %s", err)
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
		return nil, fmt.Errorf("input offers not found")
	}
	// type gymnastics
	offersBytes, err := json.Marshal(offersInterface)
	if err != nil {
		return nil, fmt.Errorf("input offers not correctly formed %s", err)
	}
	var offers []abi.Offer
	err = json.Unmarshal(offersBytes, &offers)
	if err != nil {
		return nil, err
	}

	rewardOffers := make([]*database.RewardOffer, len(offers))
	for i, offer := range offers {
		leadProvidersHex := make([]string, len(offer.LeadProviders))
		for j, provider := range offer.LeadProviders {
			leadProvidersHex[j] = provider.Hex()[2:]
		}
		providers, err := json.Marshal(leadProvidersHex)
		if err != nil {
			return nil, err
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
