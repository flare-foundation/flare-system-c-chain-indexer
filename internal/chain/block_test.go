package chain

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// serveRecorded answers every JSON-RPC request with result, echoing the
// request id, and returns a client for it.
func serveRecorded(t *testing.T, result any, concurrency int) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := DialRPCNode(u, concurrency)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// recordedBlock is the part of a recorded eth_getBlockByNumber result the
// test compares against.
type recordedBlock struct {
	Hash         common.Hash  `json:"hash"`
	Number       *hexutil.Big `json:"number"`
	Transactions []struct {
		Hash common.Hash    `json:"hash"`
		From common.Address `json:"from"`
	} `json:"transactions"`
}

// TestBlockHashIsTheNodes pins the reason this package exists: a Flare header
// hashes differently from an Ethereum one, so the hash must come from the
// node, not from go-ethereum's header type.
func TestBlockHashIsTheNodes(t *testing.T) {
	raw, err := os.ReadFile("testdata/flare_block.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded json.RawMessage = raw
	var want recordedBlock
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	c := serveRecorded(t, recorded, 1)
	block, err := c.BlockByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("BlockByNumber: %v", err)
	}

	if block.Hash() != want.Hash {
		t.Errorf("Hash() = %s, want the node's %s", block.Hash().Hex(), want.Hash.Hex())
	}
	if block.header.Hash() == want.Hash {
		t.Error("go-ethereum's header hash matches the node on a Flare block: the raw fetch is no longer needed, revisit this package")
	}
	if block.Number().Cmp(want.Number.ToInt()) != 0 {
		t.Errorf("Number() = %s, want %s", block.Number(), want.Number.ToInt())
	}
	if len(block.Transactions()) != len(want.Transactions) {
		t.Fatalf("Transactions() = %d, want %d", len(block.Transactions()), len(want.Transactions))
	}
	for i, tx := range block.Transactions() {
		if tx.Hash() != want.Transactions[i].Hash {
			t.Errorf("tx %d hash = %s, want %s", i, tx.Hash().Hex(), want.Transactions[i].Hash.Hex())
		}
		from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil || from != want.Transactions[i].From {
			t.Errorf("tx %d sender = %s (%v), want %s", i, from.Hex(), err, want.Transactions[i].From.Hex())
		}
	}
}

func TestBlockByNumberReportsAMissingBlock(t *testing.T) {
	c := serveRecorded(t, nil, 1)

	_, err := c.BlockByNumber(context.Background(), big.NewInt(1))
	if !IsBlockUnavailable(err) {
		t.Errorf("err = %v, want the not-found sentinel", err)
	}
}

// TestConcurrencyCap checks that the client never has more requests in
// flight than the configured cap, whichever method issued them.
func TestConcurrencyCap(t *testing.T) {
	var inFlight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)

		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": "0xe"})
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	c, err := DialRPCNode(u, 3)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ChainID(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if peak.Load() > 3 {
		t.Errorf("peak in-flight requests = %d, want at most 3", peak.Load())
	}
	if peak.Load() < 2 {
		t.Errorf("peak in-flight requests = %d, the cap is not the only limit", peak.Load())
	}
}

// TestSlotsSurviveErrorResponses pins the stall a rate-limiting node would
// cause if a failed request kept its slot: after rpc_concurrency rejections
// the client would block for good.
func TestSlotsSurviveErrorResponses(t *testing.T) {
	const concurrency = 2

	var reject atomic.Bool
	reject.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reject.Load() {
			http.Error(w, "too many requests", http.StatusTooManyRequests)

			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": "0xe"})
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	c, err := DialRPCNode(u, concurrency)
	if err != nil {
		t.Fatal(err)
	}

	// Every call is bounded, so a slot that is never given back fails the
	// test instead of blocking it forever.
	call := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := c.ChainID(ctx)

		return err
	}

	// More rejections than the cap: each must give its slot back.
	for i := range concurrency * 3 {
		err := call()
		if err == nil {
			t.Fatalf("rejected request %d: expected an error", i)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("rejected request %d blocked: a slot was not released", i)
		}
	}

	reject.Store(false)
	if err := call(); err != nil {
		t.Errorf("the client no longer works after rejected requests: %v", err)
	}
}

// TestBlockByNumberRejectsAnIncompleteTransactionList guards the indexer's
// core invariant: a block is stored only with every transaction it has. A
// node that reports a transactions root but sends no transactions must fail
// rather than be indexed as empty.
func TestBlockByNumberRejectsAnIncompleteTransactionList(t *testing.T) {
	raw, err := os.ReadFile("testdata/flare_block.json")
	if err != nil {
		t.Fatal(err)
	}
	var block map[string]any
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatal(err)
	}
	txs, ok := block["transactions"].([]any)
	if !ok || len(txs) == 0 {
		t.Fatal("the recorded block has no transactions to drop")
	}

	t.Run("transactions dropped", func(t *testing.T) {
		withoutTxs := maps.Clone(block)
		withoutTxs["transactions"] = []any{}

		c := serveRecorded(t, withoutTxs, 1)
		if _, err := c.BlockByNumber(context.Background(), nil); err == nil {
			t.Error("a block missing its transactions was accepted")
		}
	})

	t.Run("transactions the header does not have", func(t *testing.T) {
		emptyRoot := maps.Clone(block)
		emptyRoot["transactionsRoot"] = types.EmptyTxsHash.Hex()

		c := serveRecorded(t, emptyRoot, 1)
		if _, err := c.BlockByNumber(context.Background(), nil); err == nil {
			t.Error("transactions were accepted for a block whose header has none")
		}
	})
}
