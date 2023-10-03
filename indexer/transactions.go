package indexer

import (
	"encoding/hex"
	"encoding/json"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/abi"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func (ci *BlockIndexer) getTransactionsReceipt(transactionBatch *TransactionsBatch,
	filteredTransactionsBatch *TransactionsBatch,
	start, stop int, errChan chan error) {
	var receipt *types.Receipt
	var err error
	for i := start; i < stop; i++ {
		tx := transactionBatch.Transactions[i]
		for j := 0; j < 10; j++ {
			receipt, err = ci.client.TransactionReceipt(ci.ctx, tx.Hash())
			if err == nil {
				if j > 0 {
					fmt.Println(j)
				}
				break
			}
		}
		if err != nil {
			errChan <- err
			return
		}

		if receipt.Status == types.ReceiptStatusSuccessful {
			filteredTransactionsBatch.Lock()
			filteredTransactionsBatch.Transactions = append(filteredTransactionsBatch.Transactions, tx)
			filteredTransactionsBatch.toBlock = append(filteredTransactionsBatch.toBlock, transactionBatch.toBlock[i])
			filteredTransactionsBatch.Unlock()
		}
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
		epoch := abi.EpochFromTimeInt(block.Time(), ci.epoch.FirstEpochStartSec, ci.epoch.EpochDurationSec)
		dbTx := &database.FtsoTransaction{
			Data: txData, BlockId: block.NumberU64(),
			FuncCall:  funcCall,
			Status:    1,
			From:      fromAddress.Hex(),
			To:        tx.To().Hex(),
			Timestamp: block.Time(),
			Epoch:     epoch,
		}
		data.Transactions = append(data.Transactions, dbTx)
		parametersMap, err := abi.DecodeTxParams(tx.Data())
		if err != nil {
			return nil, err
		}

		switch funcCall {
		case abi.FtsoCommit:
			commit, err := processCommit(parametersMap, fromAddress, epoch)
			if err != nil {
				return nil, err
			}
			data.Commits = append(data.Commits, commit)
		case abi.FtsoReveal:
			reveal, err := processReveal(parametersMap, fromAddress, epoch)
			if err != nil {
				return nil, err
			}
			data.Reveals = append(data.Reveals, reveal)
		case abi.FtsoSignature:
			signatureData, err := processSignature(parametersMap, fromAddress, block.Time(), epoch)
			if err != nil {
				return nil, err
			}
			data.Signatures = append(data.Signatures, signatureData)
		case abi.FtsoFinalize:
			finalization, err := processFinalization(parametersMap, fromAddress, block.Time(), epoch)
			if err != nil {
				return nil, err
			}
			data.Finalizations = append(data.Finalizations, finalization)
		case abi.FtsoOffers:
			offers, err := processRewardOffers(parametersMap, fromAddress, block.Time(), epoch)
			if err != nil {
				return nil, err
			}
			data.RewardOffers = append(data.RewardOffers, offers...)
		}
	}

	return data, nil
}

func processCommit(parametersMap map[string]interface{}, fromAddress common.Address, epoch uint64) (*database.Commit, error) {
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
		Address:    fromAddress.Hex(),
		CommitHash: hex.EncodeToString(commitHash[:])}

	return commit, nil
}

func processReveal(parametersMap map[string]interface{}, fromAddress common.Address, epoch uint64) (*database.Reveal, error) {
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
		Address:    fromAddress.Hex(),
		Random:     hex.EncodeToString(random[:]),
		MerkleRoot: hex.EncodeToString(merkleRoot[:]),
		BitVote:    hex.EncodeToString(bitVote),
		Prices:     hex.EncodeToString(prices),
	}

	return reveal, nil
}

func processSignature(parametersMap map[string]interface{}, fromAddress common.Address,
	timestamp uint64, blockEpoch uint64) (*database.SignatureData, error) {
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
		Address:        fromAddress.Hex(),
		MerkleRoot:     hex.EncodeToString(merkleRoot[:]),
		Signature:      string(signature),
		Timestamp:      timestamp,
	}

	return signatureData, nil
}

func processFinalization(parametersMap map[string]interface{}, fromAddress common.Address,
	timestamp uint64, blockEpoch uint64) (*database.Finalization, error) {
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
		Address:        fromAddress.Hex(),
		MerkleRoot:     hex.EncodeToString(merkleRoot[:]),
		Signatures:     string(signatures),
		Timestamp:      timestamp,
	}

	return finalization, nil
}

func processRewardOffers(parametersMap map[string]interface{}, fromAddress common.Address,
	timestamp uint64, blockEpoch uint64) ([]*database.RewardOffer, error) {
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
			leadProvidersHex[j] = provider.Hex()
		}
		providers, err := json.Marshal(leadProvidersHex)
		if err != nil {
			return nil, err
		}
		rewardOffers[i] = &database.RewardOffer{
			Epoch:               blockEpoch,
			Address:             fromAddress.Hex(),
			Amount:              offer.Amount.Uint64(),
			CurrencyAddress:     offer.CurrencyAddress.Hex(),
			OfferSymbol:         hex.EncodeToString(offer.OfferSymbol[:]),
			QuoteSymbol:         hex.EncodeToString(offer.QuoteSymbol[:]),
			LeadProviders:       string(providers),
			RewardBeltPPM:       offer.RewardBeltPPM.Uint64(),
			ElasticBandWidthPPM: offer.RewardBeltPPM.Uint64(),
			IqrSharePPM:         offer.IqrSharePPM.Uint64(),
			PctSharePPM:         offer.PctSharePPM.Uint64(),
			RemainderClaimer:    offer.RemainderClaimer.Hex(),
		}
	}

	return rewardOffers, nil
}
