package snapshot

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// Snapshot represents a point-in-time view of the DB
type DBSnapshot struct {
	ID        uint64
	CreatedAt time.Time
	MaxOffset int64
}

// Manager handles all snapshots for a DB
type Manager struct {
	mu        sync.Mutex
	file      *os.File
	nextID    uint64
	snapshots []Snapshot
}

// Open opens or creates a snapshot manager file
func OpenSnapshotManager(path string) (*Manager, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	m := &Manager{file: file}
	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

// load reads all snapshots from the file
func (m *Manager) load() error {
	stat, err := m.file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		return nil
	}

	for {
		var id uint64
		var ts int64
		var maxOffset int64

		// Read ID
		if err := binary.Read(m.file, binary.LittleEndian, &id); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		// Read timestamp
		if err := binary.Read(m.file, binary.LittleEndian, &ts); err != nil {
			return err
		}

		// Read max offset
		if err := binary.Read(m.file, binary.LittleEndian, &maxOffset); err != nil {
			return err
		}

		m.snapshots = append(m.snapshots, Snapshot{
			ID:        id,
			CreatedAt: time.Unix(ts, 0),
			MaxOffset: maxOffset,
		})

		if id >= m.nextID {
			m.nextID = id + 1
		}
	}

	return nil
}

// Create creates a new snapshot at the given maxOffset
func (m *Manager) Create(maxOffset int64) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := Snapshot{
		ID:        m.nextID,
		CreatedAt: time.Now(),
		MaxOffset: maxOffset,
	}

	// Persist snapshot to file
	if err := binary.Write(m.file, binary.LittleEndian, snap.ID); err != nil {
		return snap, err
	}
	if err := binary.Write(m.file, binary.LittleEndian, snap.CreatedAt.Unix()); err != nil {
		return snap, err
	}
	if err := binary.Write(m.file, binary.LittleEndian, snap.MaxOffset); err != nil {
		return snap, err
	}

	if err := m.file.Sync(); err != nil {
		return snap, err
	}

	// Add to in-memory list
	m.snapshots = append(m.snapshots, snap)
	m.nextID++

	return snap, nil
}

// List returns a copy of all snapshots
func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Snapshot(nil), m.snapshots...)
}

// Close closes the snapshot manager file
func (m *Manager) Close() error {
	return m.file.Close()
}
