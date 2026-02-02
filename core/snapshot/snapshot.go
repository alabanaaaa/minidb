package snapshot

import "time"

type Snapshot struct {
	ID        uint64
	CreatedAt time.Time
	MaxOffset int64
	Offset    int64
	Index     map[string]int64
}
