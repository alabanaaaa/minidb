package minidatabase

import (
	"errors"
	"io"
	"os"
	"sync"
)

type DB struct {
	storage *Storage
	index   map[string]int64
	mu      sync.RWMutex // protects index and storage
}

// OpenDB opens the storage engine and creates an empty index
func OpenDB(path string) (*DB, error) {
	storage, err := OpenStorage(path)
	if err != nil {
		return nil, err
	}

	db := &DB{
		storage: storage,
		index:   make(map[string]int64),
	}

	// Replay existing data to build the in-memory index
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

// Get retrieves the value for a given key
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

	if string(rec.Key) != key {
		return "", errors.New("key mismatch")
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

// Compact rewrites the db file to remove deleted record
func (db *DB) Compact() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tmpPath := db.storage.path + ".compact"
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

// Close safely closes the storage file
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.storage.Close()
}
