package health

import (
	"flare-ftso-indexer/internal/logger"
	"flare-ftso-indexer/internal/ready"
	"net/http"
)

const listenAddress = ":8080"

// Start launches an HTTP health endpoint on /health.
// The endpoint returns:
//   - 503 while the indexer is still catching up at startup
//   - 200 once startup backfill is complete and continuous indexing begins
func Start() {
	go func() {
		err := http.ListenAndServe(listenAddress, handler())
		if err != nil {
			logger.Error("Health server error: %s", err)
		}
	}()

	logger.Info("Health endpoint available at http://0.0.0.0%s/health", listenAddress)
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
