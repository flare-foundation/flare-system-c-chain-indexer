// Package chain is the indexer's client for a Flare C-chain node. It wraps
// go-ethereum's ethclient with a cap on simultaneous requests and with the
// block handling Flare needs.
//
// Flare block headers carry extra fields (extDataHash, blockGasCost,
// extDataGasUsed) that go-ethereum does not know, so it computes block hashes
// that do not match the chain. Blocks therefore come from BlockByNumber, which
// keeps the hash the node reported, and Block exposes no other one. Headers,
// transactions, receipts and logs need no special handling.
package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
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

// IsBlockUnavailable reports whether err is the node saying it does not have the
// block: a null result, which the client surfaces as ethereum.NotFound.
// Callers use it to skip retrying an answer that cannot change.
//
// Error text is deliberately not matched, since transport failures carry prose
// that reads like absence — "503 Service Unavailable", a proxy's "404 not found".
// Such an error is simply retried instead.
func IsBlockUnavailable(err error) bool {
	return errors.Is(err, ethereum.NotFound)
}

// The generated contract bindings read through Client, so it has to keep
// satisfying this interface.
var _ bind.ContractCaller = (*Client)(nil)

// Client is the node client the indexer uses. Every method goes through one
// cap on simultaneous requests, so catchup, continuous indexing, the FSP
// backfill, the start-block search, contract calls and history drop together
// never exceed indexer.rpc_concurrency in flight.
//
// The methods are written out rather than promoted from an embedded
// ethclient, so a call the indexer has not wrapped does not compile instead
// of quietly escaping the cap.
type Client struct {
	eth *ethclient.Client
	rpc *rpc.Client
	sem chan struct{}
}

// Block is a block as the node reported it. Its hash is the node's, not one
// recomputed from the header.
type Block struct {
	hash   common.Hash
	header *types.Header
	txs    []*types.Transaction
}

func (b *Block) Hash() common.Hash { return b.hash }
func (b *Block) Time() uint64      { return b.header.Time }

// Number returns a copy, so a caller cannot alter the header the block keeps.
func (b *Block) Number() *big.Int { return new(big.Int).Set(b.header.Number) }

func (b *Block) Transactions() []*types.Transaction { return b.txs }

// DialRPCNode connects to the node and caps simultaneous requests at
// maxConcurrency. Values below 1 are treated as 1.
func DialRPCNode(nodeURL *url.URL, maxConcurrency int) (*Client, error) {
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}

	rc, err := rpc.DialContext(context.Background(), nodeURL.String())
	if err != nil {
		return nil, err
	}

	return &Client{
		eth: ethclient.NewClient(rc),
		rpc: rc,
		sem: make(chan struct{}, maxConcurrency),
	}, nil
}

// acquire blocks until a request slot is free or ctx is done.
func (c *Client) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() {
	<-c.sem
}

func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	return c.eth.ChainID(ctx)
}

func (c *Client) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	return c.eth.HeaderByNumber(ctx, number)
}

func (c *Client) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	return c.eth.TransactionReceipt(ctx, txHash)
}

func (c *Client) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	return c.eth.FilterLogs(ctx, q)
}

// CodeAt and CallContract are what bind.ContractCaller needs, so contract
// reads count against the cap like every other request.

func (c *Client) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	return c.eth.CodeAt(ctx, contract, blockNumber)
}

func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	return c.eth.CallContract(ctx, call, blockNumber)
}

// BlockByNumber fetches a block with its transactions. A nil number means the
// latest block. The returned block carries the hash the node reported for it.
//
// Flare block headers carry extra fields, so a hash computed from the header
// alone is wrong. The response is decoded here to keep the node's hash.
func (c *Client) BlockByNumber(ctx context.Context, number *big.Int) (*Block, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	var raw json.RawMessage
	if err := c.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", blockNumberArg(number), true); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ethereum.NotFound
	}

	var header types.Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("decoding block header: %w", err)
	}

	var body struct {
		Hash         common.Hash          `json:"hash"`
		Transactions []*types.Transaction `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("decoding block body: %w", err)
	}
	if body.Hash == (common.Hash{}) {
		return nil, errors.New("block without a hash in the node's response")
	}

	// The header says whether the block has transactions, so a response whose
	// list disagrees with it is incomplete. Without this the block would be
	// indexed as empty and indexing would move past it, losing its
	// transactions and their logs with no sign that anything went missing.
	if header.TxHash != types.EmptyTxsHash && len(body.Transactions) == 0 {
		return nil, errors.New("node reported a block with transactions but sent none")
	}
	if header.TxHash == types.EmptyTxsHash && len(body.Transactions) > 0 {
		return nil, errors.New("node sent transactions for a block whose header has none")
	}

	return &Block{hash: body.Hash, header: &header, txs: body.Transactions}, nil
}

func blockNumberArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}

	return hexutil.EncodeBig(number)
}
