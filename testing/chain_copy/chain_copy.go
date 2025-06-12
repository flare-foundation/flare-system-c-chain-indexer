package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
)

type PostToChain struct {
	Method  string        `json:"method"`
	Id      int           `json:"id"`
	Jsonrpc string        `json:"jsonrpc"`
	Params  []interface{} `json:"params"`
}

func CopyChain(
	ctx context.Context, address string, start, stop int,
) (map[int][]byte, map[string][]byte, error) {
	client := &http.Client{}
	blockDict := make(map[int][]byte)
	txDict := make(map[string][]byte)
	// get block responses
	for i := start; i < stop; i++ {
		iHex := fmt.Sprintf("0x%x", i)
		req := PostToChain{Method: "eth_getBlockByNumber", Id: 31337, Jsonrpc: "2.0", Params: []interface{}{iHex, true}}
		reqBytes, err := json.Marshal(req)
		if err != nil {
			return nil, nil, err
		}

		r, err := http.NewRequest("POST", address, bytes.NewBuffer(reqBytes))
		if err != nil {
			return nil, nil, err
		}
		r.Close = true
		r.Header.Add("Content-Type", "application/json")
		res, err := client.Do(r)
		if err != nil {
			return nil, nil, err
		}

		defer func() {
			err := res.Body.Close()
			if err != nil {
				fmt.Println("Error closing response body:", err)
			}
		}()

		if res.StatusCode != 200 {
			return nil, nil, errors.Errorf("error response")
		}

		b, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, nil, errors.Errorf("error reading")
		}
		blockDict[i] = b
	}

	// get transaction responses
	ethClient, err := ethclient.Dial(address)
	if err != nil {
		return nil, nil, err
	}

	for i := start; i < stop; i++ {
		block, err := ethClient.BlockByNumber(ctx, big.NewInt(int64(i)))
		if err != nil {
			return nil, nil, err
		}
		for _, tx := range block.Transactions() {
			hashHex := fmt.Sprintf("0x%x", tx.Hash())
			req := PostToChain{Method: "eth_getTransactionReceipt", Id: 31337, Jsonrpc: "2.0", Params: []interface{}{hashHex}}
			reqBytes, err := json.Marshal(req)
			if err != nil {
				return nil, nil, err
			}

			r, err := http.NewRequest("POST", address, bytes.NewBuffer(reqBytes))
			if err != nil {
				return nil, nil, err
			}
			r.Close = true
			r.Header.Add("Content-Type", "application/json")
			res, err := client.Do(r)
			if err != nil {
				return nil, nil, err
			}

			defer func() {
				err := res.Body.Close()
				if err != nil {
					fmt.Println("Error closing response body:", err)
				}
			}()

			if res.StatusCode != 200 {
				return nil, nil, errors.Errorf("error response")
			}

			b, err := io.ReadAll(res.Body)
			if err != nil {
				return nil, nil, errors.Errorf("error reading")
			}
			txDict[hashHex] = b
		}
	}

	return blockDict, txDict, nil
}

// Assuming that the chain is running on 8545, this function will copy all the info needed
// to replay the chain.
func main() {
	ctx := context.Background()

	blockDict, txDict, err := CopyChain(ctx, "http://localhost:8545", 0, 2500)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	blockContent, err := json.Marshal(blockDict)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	err = os.WriteFile("blocks.json", blockContent, 0644)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	txContent, err := json.Marshal(txDict)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	err = os.WriteFile("transactions.json", txContent, 0644)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
}
