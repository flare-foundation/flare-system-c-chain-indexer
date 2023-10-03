package abi

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type Offer struct {
	Amount              *big.Int         "json:\"amount\""
	CurrencyAddress     common.Address   "json:\"currencyAddress\""
	OfferSymbol         [4]uint8         "json:\"offerSymbol\""
	QuoteSymbol         [4]uint8         "json:\"quoteSymbol\""
	LeadProviders       []common.Address "json:\"leadProviders\""
	RewardBeltPPM       *big.Int         "json:\"rewardBeltPPM\""
	ElasticBandWidthPPM *big.Int         "json:\"elasticBandWidthPPM\""
	IqrSharePPM         *big.Int         "json:\"iqrSharePPM\""
	PctSharePPM         *big.Int         "json:\"pctSharePPM\""
	RemainderClaimer    common.Address   "json:\"remainderClaimer\""
}
