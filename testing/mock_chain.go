package testing

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

var ChainLastBlock = 0

func MockChain(port int, blockFile, txFile string) error {
	file, err := os.ReadFile(blockFile)
	if err != nil {
		return err
	}
	var blockDict map[int][]byte
	err = json.Unmarshal(file, &blockDict)
	if err != nil {
		return err
	}
	for key := range blockDict {
		ChainLastBlock = max(ChainLastBlock, key)
	}

	file, err = os.ReadFile(txFile)
	if err != nil {
		return err
	}
	var txDict map[string][]byte
	err = json.Unmarshal(file, &txDict)
	if err != nil {
		return err
	}

	r := mux.NewRouter()

	r.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		ChainMockResponses(writer, request, blockDict, txDict)
	})

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(port),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      r,
	}

	fmt.Println("Mock server starting")
	err = server.ListenAndServe()

	return err
}

func ChainMockResponses(writer http.ResponseWriter, request *http.Request, blockDict map[int][]byte, txDict map[string][]byte) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "Invalid request body", http.StatusBadRequest)
		return
	}

	var form map[string]json.RawMessage
	err = json.Unmarshal(body, &form)
	if err != nil {
		fmt.Printf("Error unmarshaling json: %v\n", err)
		http.Error(writer, "Invalid json", http.StatusBadRequest)
		return
	}

	var params []interface{}
	err = json.Unmarshal(form["params"], &params)
	if err != nil {
		fmt.Printf("Error unmarshaling params: %v\n", err)
		http.Error(writer, "Invalid json", http.StatusBadRequest)
		return
	}

	methodBytes, err := form["method"].MarshalJSON()
	if err != nil {
		fmt.Printf("Error marshaling method: %v\n", err)
		http.Error(writer, "Invalid method", http.StatusBadRequest)
		return
	}

	var method string
	err = json.Unmarshal(methodBytes, &method)
	if err != nil {
		fmt.Printf("Error unmarshaling method: %v\n", err)
		http.Error(writer, "Invalid method", http.StatusInternalServerError)
		return
	}

	if method == "eth_getBlockByNumber" {
		iBig := new(big.Int)
		if params[0].(string) == "latest" {
			iBig.SetInt64(int64(ChainLastBlock))
		} else {
			iBig.SetString(params[0].(string)[2:], 16)
		}
		_, err = writer.Write(blockDict[int(iBig.Int64())])
		if err != nil {
			fmt.Printf("Error returning block: %v\n", err)
		}
	} else if method == "eth_getTransactionReceipt" {
		_, err = writer.Write(txDict[params[0].(string)])
		if err != nil {
			fmt.Printf("Error returning block: %v\n", err)
		}
	}

}
