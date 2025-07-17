package chain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"

	"flare-ftso-indexer/logger"

	avxClient "github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethClient "github.com/ethereum/go-ethereum/ethclient"

	avxTypes "github.com/ava-labs/coreth/core/types"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

// ChainID represents the external chain ID which identifies a particular
// blockchain network.
type ChainID int

const (
	ChainIDFlare    ChainID = 14
	ChainIDSongbird ChainID = 19
	ChainIDCoston   ChainID = 16
	ChainIDCoston2  ChainID = 114
)

func ChainIDFromBigInt(chainID *big.Int) ChainID {
	return ChainID(chainID.Int64())
}

// ChainType is an internal type used to differentiate between different
// types of EVM-compatible chains.
type ChainType int

const (
	ChainTypeAvax ChainType = iota + 1 // Add 1 to skip 0 - avoids the zero value defaulting to Avax
	ChainTypeEth
)

type Client struct {
	chain ChainType
	eth   *ethClient.Client
	avx   avxClient.Client
}

type Block struct {
	chain ChainType
	eth   *ethTypes.Block
	avx   *avxTypes.Block
}

type Header struct {
	chain ChainType
	eth   *ethTypes.Header
	avx   *avxTypes.Header
}

type Receipt struct {
	chain ChainType
	eth   *ethTypes.Receipt
	avx   *avxTypes.Receipt
}

type Transaction struct {
	chain ChainType
	eth   *ethTypes.Transaction
	avx   *avxTypes.Transaction
}

func DialRPCNode(nodeURL *url.URL, chainType ChainType) (*Client, error) {
	c := &Client{chain: chainType}
	var err error

	switch c.chain {
	case ChainTypeAvax:
		c.avx, err = avxClient.Dial(nodeURL.String())
	case ChainTypeEth:
		c.eth, err = ethClient.Dial(nodeURL.String())
	default:
		return nil, errors.New("invalid chain")
	}

	return c, err
}

func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	switch c.chain {
	case ChainTypeAvax:
		return c.avx.ChainID(ctx)
	case ChainTypeEth:
		return c.eth.ChainID(ctx)
	default:
		return nil, errors.New("invalid chain")
	}
}

func (c *Client) BlockByNumber(ctx context.Context, number *big.Int) (*Block, error) {
	block := &Block{chain: c.chain}
	var err error
	switch c.chain {
	case ChainTypeAvax:
		block.avx, err = c.avx.BlockByNumber(ctx, number)
	case ChainTypeEth:
		block.eth, err = c.eth.BlockByNumber(ctx, number)
	default:
		return nil, errors.New("invalid chain")
	}

	return block, err
}

func (c *Client) HeaderByNumber(ctx context.Context, number *big.Int) (*Header, error) {
	block := &Header{chain: c.chain}
	var err error
	switch c.chain {
	case ChainTypeAvax:
		block.avx, err = c.avx.HeaderByNumber(ctx, number)
	case ChainTypeEth:
		block.eth, err = c.eth.HeaderByNumber(ctx, number)
	default:
		return nil, errors.New("invalid chain")
	}

	return block, err
}

func (c *Client) TransactionReceipt(ctx context.Context, txHash common.Hash) (*Receipt, error) {
	receipt := &Receipt{chain: c.chain}
	var err error
	switch c.chain {
	case ChainTypeAvax:
		receipt.avx, err = c.avx.TransactionReceipt(ctx, txHash)
	case ChainTypeEth:
		receipt.eth, err = c.eth.TransactionReceipt(ctx, txHash)
	default:
		return nil, errors.New("invalid chain")
	}

	return receipt, err
}

func (c *Client) FilterLogs(ctx context.Context, q interfaces.FilterQuery) ([]avxTypes.Log, error) {
	switch c.chain {
	case ChainTypeAvax:
		return c.avx.FilterLogs(ctx, q)
	case ChainTypeEth:
		ethLogs, err := c.eth.FilterLogs(ctx, ethereum.FilterQuery(q))
		if err != nil {
			return nil, err
		}
		logs := make([]avxTypes.Log, len(ethLogs))
		for i, e := range ethLogs {
			logs[i] = avxTypes.Log(e)
		}
		return logs, nil

	default:
		return nil, errors.New("invalid chain")
	}
}

func (b *Header) Number() *big.Int {
	switch b.chain {
	case ChainTypeAvax:
		return b.avx.Number
	case ChainTypeEth:
		return b.eth.Number
	default:
		return nil
	}
}

func (b *Header) Time() uint64 {
	switch b.chain {
	case ChainTypeAvax:
		return b.avx.Time
	case ChainTypeEth:
		return b.eth.Time
	default:
		return 0
	}
}

func (b *Block) Number() *big.Int {
	switch b.chain {
	case ChainTypeAvax:
		return b.avx.Number()
	case ChainTypeEth:
		return b.eth.Number()
	default:
		return nil
	}
}

func (b *Block) Time() uint64 {
	switch b.chain {
	case ChainTypeAvax:
		return b.avx.Time()
	case ChainTypeEth:
		return b.eth.Time()
	default:
		return 0
	}
}

func (b *Block) Hash() common.Hash {
	switch b.chain {
	case ChainTypeAvax:
		return b.avx.Hash()
	case ChainTypeEth:
		return b.eth.Hash()
	default:
		return common.Hash{}
	}
}

func (r *Receipt) Status() uint64 {
	switch r.chain {
	case ChainTypeAvax:
		return r.avx.Status
	case ChainTypeEth:
		return r.eth.Status
	default:
		return 0
	}
}

func (r *Receipt) Logs() []*avxTypes.Log {
	switch r.chain {
	case ChainTypeAvax:
		return r.avx.Logs
	case ChainTypeEth:
		logs := make([]*avxTypes.Log, len(r.eth.Logs))
		for i, e := range r.eth.Logs {
			log := avxTypes.Log(*e)
			logs[i] = &log
		}
		return logs
	default:
		return nil
	}
}

func (b *Block) Transactions() []*Transaction {
	switch b.chain {
	case ChainTypeAvax:
		txsAvx := b.avx.Transactions()
		txs := make([]*Transaction, len(txsAvx))
		for i, e := range txsAvx {
			txs[i] = &Transaction{}
			txs[i].chain = b.chain
			txs[i].avx = e
		}
		return txs
	case ChainTypeEth:
		txsEth := b.eth.Transactions()
		txs := make([]*Transaction, len(txsEth))
		for i, e := range txsEth {
			txs[i] = &Transaction{}
			txs[i].chain = b.chain
			txs[i].eth = e
		}
		return txs
	default:
		return nil
	}
}

func (t *Transaction) Hash() common.Hash {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.Hash()
	case ChainTypeEth:
		return t.eth.Hash()
	default:
		return common.Hash{}
	}
}

func (t *Transaction) To() *common.Address {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.To()
	case ChainTypeEth:
		return t.eth.To()
	default:
		return nil
	}
}

func (t *Transaction) Data() []byte {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.Data()
	case ChainTypeEth:
		return t.eth.Data()
	default:
		return nil
	}
}

func (t *Transaction) ChainId() *big.Int {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.ChainId()
	case ChainTypeEth:
		return t.eth.ChainId()
	default:
		return nil
	}
}

func (t *Transaction) Value() *big.Int {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.Value()
	case ChainTypeEth:
		return t.eth.Value()
	default:
		return nil
	}
}

func (t *Transaction) GasPrice() *big.Int {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.GasPrice()
	case ChainTypeEth:
		return t.eth.GasPrice()
	default:
		return nil
	}
}

func (t *Transaction) Gas() uint64 {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.Gas()
	case ChainTypeEth:
		return t.eth.Gas()
	default:
		return 0
	}
}

func (t *Transaction) FromAddress() (common.Address, error) {
	switch t.chain {
	case ChainTypeAvax:
		return avxTypes.Sender(avxTypes.LatestSignerForChainID(t.avx.ChainId()), t.avx)
	case ChainTypeEth:
		return ethTypes.Sender(ethTypes.LatestSignerForChainID(t.eth.ChainId()), t.eth)
	default:
		return common.Address{}, fmt.Errorf("wrong chain")
	}
}

func (t *Transaction) RawSignatureValues() (v, r, s *big.Int) {
	switch t.chain {
	case ChainTypeAvax:
		return t.avx.RawSignatureValues()
	case ChainTypeEth:
		return t.eth.RawSignatureValues()
	default:
		logger.Error("RawSignatureValues called on unsupported chain type: %d", t.chain)
		return nil, nil, nil
	}
}
