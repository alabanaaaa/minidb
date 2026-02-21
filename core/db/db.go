package db

import (
	"errors"
	"io"
	"os"
	"sync"

	snapshotpkg "mini-database/core/snapshot"
)

type DB struct {
	storage   *Storage
	index     map[string]int64
	snapshots *snapshotpkg.Manager
	mu        sync.RWMutex // protects index and storage
}

// OpenDB opens the storage engine and builds the in-memory index
func OpenDB(path string) (*DB, error) {
	storage, err := OpenStorage(path)
	if err != nil {
		return nil, err
	}

	db := &DB{
		storage: storage,
		index:   make(map[string]int64),
	}

	// Open snapshot manager
	mgr, err := snapshotpkg.OpenSnapshotManager(storage.path + ".snap")
	if err != nil {
		db.storage.Close()
		return nil, err
	}
	db.snapshots = mgr

	// Replay data to build in-memory index
	if err := db.Replay(); err != nil {
		return nil, err
	}

	return db, nil
}

// Put inserts or updates a key-value pair
func (db *DB) Put(key, value string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	offset, err := db.storage.Append([]byte(key), []byte(value), false)
	if err != nil {
		return err
	}

	db.index[key] = offset
	return nil
}

// Get retrieves a value for a key
func (db *DB) Get(key string) (string, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	offset, ok := db.index[key]
	if !ok {
		return "", errors.New("key not found")
	}

	rec, _, err := db.storage.ReadAt(offset)
	if err != nil {
		return "", err
	}

	if rec.Tombstone {
		return "", errors.New("key deleted")
	}

	return string(rec.Value), nil
}

// Delete marks a key as deleted
func (db *DB) Delete(key string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.storage.Append([]byte(key), nil, true)
	if err != nil {
		return err
	}

	delete(db.index, key)
	return nil
}

// Replay rebuilds the in-memory index from the storage file
func (db *DB) Replay() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	var offset int64 = 0
	for {
		rec, n, err := db.storage.ReadAt(offset)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}

		key := string(rec.Key)
		if rec.Tombstone {
			delete(db.index, key)
		} else {
			db.index[key] = offset
		}
		offset += n
	}
}

// Compact rewrites the DB file to remove deleted records
func (db *DB) Compact() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tmpPath := db.storage.path + ".compact.tmp"
	newStorage, err := OpenStorage(tmpPath)
	if err != nil {
		return err
	}
	defer newStorage.Close()

	newIndex := make(map[string]int64)
	for key, offset := range db.index {
		rec, _, err := db.storage.ReadAt(offset)
		if err != nil {
			return err
		}

		if rec.Tombstone {
			continue
		}

		newOffset, err := newStorage.Append(rec.Key, rec.Value, false)
		if err != nil {
			return err
		}
		newIndex[key] = newOffset
	}

	db.storage.Close()
	if err := os.Rename(tmpPath, db.storage.path); err != nil {
		return err
	}

	storage, err := OpenStorage(db.storage.path)
	if err != nil {
		return err
	}

	db.storage = storage
	db.index = newIndex
	return nil
}

// CreateSnapshot creates a snapshot of the current DB state
func (db *DB) CreateSnapshot() (*snapshotpkg.Snapshot, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	size, err := db.storage.Size()
	if err != nil {
		return nil, err
	}

	indexCopy := make(map[string]int64, len(db.index))
	for k, v := range db.index {
		indexCopy[k] = v
	}

	if len(indexCopy) == 0 {
		panic("snapshot index is empty")
	}

	snap, err := db.snapshots.Create(size, indexCopy)
	if err != nil {
		return nil, err
	}

	// also expose current file offset for callers

	return &snap, nil
}

// ReadAtSnapshot reads a key as it existed at a given snapshot
func (db *DB) ReadAtSnapshot(key string, snap *snapshotpkg.Snapshot) (string, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	offset, ok := snap.Index[key]
	if !ok {
		return "", errors.New("key not found at snapshot")
	}

	rec, _, err := db.storage.ReadAt(offset)
	if err != nil {
		return "", err
	}

	if rec.Tombstone {
		return "", errors.New("Key deleted at snapshot")

	}

	return string(rec.Value), nil
}

// Close safely closes the DB and snapshot manager
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.snapshots != nil {
		_ = db.snapshots.Close()
	}
	return db.storage.Close()
}
