package health

import (
	"fmt"
	"net"
	"net/http"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/ready"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
)

const listenAddress = ":8080"

// Start binds the HTTP health endpoint and serves it in the background.
// The endpoint returns:
//   - 503 while the indexer is still catching up at startup
//   - 200 once startup backfill is complete and continuous indexing begins
//
// A port that cannot be bound is a startup failure: an indexer without a
// health endpoint cannot be probed, so it does not start.
func Start() error {
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("bind the health endpoint on %s: %w", listenAddress, err)
	}

	go func() {
		if err := http.Serve(listener, handler()); err != nil {
			logger.Fatalw("Health endpoint stopped", "address", listenAddress, "error", err)
		}
	}()

	logger.Infow("Health endpoint listening", "address", listenAddress, "path", "/health")

	return nil
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !ready.IsSynced() {
			http.Error(w, "false", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("true\n"))
	})

	return mux
}
