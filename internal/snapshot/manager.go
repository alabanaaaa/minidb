package snapshot

import (
	"encoding/binary"
	"os"
	"sync"
	"time"
)

type Manager struct {
	mu        sync.Mutex
	file      *os.File
	nextID    uint64
	snapshots []Snapshot
}

func Open(path string) (*Manager, error) {
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
		var offset int64

		err := binary.Read(m.file, binary.LittleEndian, &id)
		if err != nil {
			return nil
		}

		binary.Read(m.file, binary.LittleEndian, &ts)
		binary.Read(m.file, binary.LittleEndian, &offset)

		m.snapshots = append(m.snapshots, Snapshot{
			ID:        id,
			CreatedAt: time.Unix(ts, 0),
			MaxOffset: offset,
		})

		if id >= m.nextID {
			m.nextID = id + 1
		}
	}
}

func (m *Manager) Create(maxOffset int64) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := Snapshot{
		ID:        m.nextID,
		CreatedAt: time.Now(),
		MaxOffset: maxOffset,
	}

	if err := binary.Write(m.file, binary.LittleEndian, snap.ID); err != nil {
		return snap, err
	}

	if err := binary.Write(m.file, binary.LittleEndian, snap.CreatedAt.Unix()); err != nil {
		return snap, err
	}

	if err := m.file.Sync(); err != nil {
		return snap, err
	}

	m.snapshots = append(m.snapshots, snap)
	m.nextID++

	return snap, nil
}

func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Snapshot(nil), m.snapshots...)
}

func (m *Manager) Close() error {
	return m.file.Close()
}
