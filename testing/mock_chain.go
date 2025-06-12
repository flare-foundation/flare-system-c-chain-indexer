package testing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flare-ftso-indexer/logger"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type MockChain struct {
	client          *http.Client
	mu              *sync.Mutex
	recorderNodeURL string
	responses       map[[sha256.Size]byte][]byte
	responsesFile   string
	server          *http.Server
}

func NewMockChain(port int, responsesFile string, recorderNodeURL string) (*MockChain, error) {
	mock := &MockChain{
		mu: new(sync.Mutex),
		server: &http.Server{
			Addr:         ":" + strconv.Itoa(port),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		recorderNodeURL: recorderNodeURL,
		responsesFile:   responsesFile,
	}

	r := mux.NewRouter()

	if recorderNodeURL == "" {
		logger.Info("running in test mode")
		if err := mock.loadResponses(); err != nil {
			return nil, err
		}

		r.HandleFunc("/", mock.ChainMockResponses)
	} else {
		logger.Info("running in recorder mode with node %s", recorderNodeURL)
		mock.client = new(http.Client)
		mock.responses = make(map[[sha256.Size]byte][]byte)

		r.HandleFunc("/", mock.RecordResponses)
	}

	mock.server.Handler = r

	return mock, nil
}

func (m *MockChain) Run(ctx context.Context) error {
	logger.Info("Mock server starting")
	err := m.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

var shutdownTimeout = 10 * time.Second

func (m *MockChain) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := m.server.Shutdown(ctx); err != nil {
		return err
	}

	logger.Info("mock chain server shutdown")

	if m.recorderNodeURL != "" {
		if err := m.writeResponses(); err != nil {
			return err
		}
	}

	logger.Info("written updated responses")

	return nil
}

type rpcRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

func (m *MockChain) ChainMockResponses(
	writer http.ResponseWriter,
	request *http.Request,
) {
	defer func() {
		err := request.Body.Close()
		if err != nil {
			logger.Error("error closing request body:", err)
		}
	}()

	reqBody, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "error reading request body", http.StatusInternalServerError)
		return
	}

	hash, err := getRequestHash(reqBody)
	if err != nil {
		http.Error(writer, "error getting request hash", http.StatusInternalServerError)
		return
	}

	if len(m.responses) == 0 {
		logger.Fatal("no responses loaded")
	}

	m.mu.Lock()
	rspData, ok := m.responses[hash]
	m.mu.Unlock()
	if !ok {
		http.Error(writer, "response not found", http.StatusNotFound)
		return
	}

	writer.Header().Add("Content-Type", "application/json")
	_, err = writer.Write(rspData)
	if err != nil {
		logger.Error("error writing response:", err)
	}
}

func (m *MockChain) RecordResponses(
	writer http.ResponseWriter,
	request *http.Request,
) {
	defer func() {
		err := request.Body.Close()
		if err != nil {
			logger.Error("error closing request body:", err)
		}
	}()

	reqBody, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "error reading request body", http.StatusInternalServerError)
		return
	}

	hash, err := getRequestHash(reqBody)
	if err != nil {
		http.Error(writer, "error getting request hash", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(request.Method, m.recorderNodeURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(writer, "error creating request", http.StatusInternalServerError)
		return
	}

	req.Header.Add("Content-Type", "application/json")

	rsp, err := new(http.Client).Do(req)
	if err != nil {
		http.Error(writer, "error making request", http.StatusInternalServerError)
		return
	}

	defer func() {
		err := rsp.Body.Close()
		if err != nil {
			logger.Error("error closing response body:", err)
		}
	}()

	if rsp.StatusCode != http.StatusOK {
		http.Error(writer, rsp.Status, rsp.StatusCode)
		return
	}

	rspBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		http.Error(writer, "error reading rsp body", http.StatusInternalServerError)
		return
	}

	m.mu.Lock()
	m.responses[hash] = rspBody
	m.mu.Unlock()

	writer.Header().Add("Content-Type", "application/json")
	_, err = writer.Write(rspBody)
	if err != nil {
		logger.Error("error writing rsp body:", err)
	}
}

func getRequestHash(reqBody []byte) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte

	var rpcReq rpcRequest
	if err := json.Unmarshal(reqBody, &rpcReq); err != nil {
		return zero, err
	}

	reqData, err := json.Marshal(rpcReq)
	if err != nil {
		return zero, err
	}

	return sha256.Sum256(reqData), nil
}

func (m *MockChain) loadResponses() error {
	logger.Info("opening %s", m.responsesFile)

	file, err := os.ReadFile(m.responsesFile)
	if err != nil {
		return err
	}

	var hexResponses map[string]json.RawMessage
	if err := json.Unmarshal(file, &hexResponses); err != nil {
		return err
	}

	responses := make(map[[sha256.Size]byte][]byte, len(hexResponses))
	for hexKey, val := range hexResponses {
		var key [sha256.Size]byte

		n, err := hex.Decode(key[:], []byte(hexKey))
		if err != nil {
			return err
		}
		if n != sha256.Size {
			return errors.New("unexpected encoded hash size")
		}

		responses[key] = val
	}

	m.responses = responses
	return nil
}

func (m *MockChain) writeResponses() error {
	responsesHex := make(map[string]json.RawMessage, len(m.responses))
	for k, v := range m.responses {
		responsesHex[hex.EncodeToString(k[:])] = v
	}

	responsesData, err := json.Marshal(responsesHex)
	if err != nil {
		return err
	}

	return os.WriteFile(m.responsesFile, responsesData, 0666)
}
