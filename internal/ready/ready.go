package ready

import "sync/atomic"

var synced atomic.Bool

func SetSynced(value bool) {
	synced.Store(value)
}

func IsSynced() bool {
	return synced.Load()
}
