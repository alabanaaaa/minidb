package minidatabase

import (
	"fmt"
)

type DB struct {
	storage *Storage
	index   map[string]int64
}

//OpenDB opens the storage engine and creates an empty index

func OpenDB(path string) (*DB, error) {
	storage, err := OpenStorage(path)
	if err != nil {
		return nil, err
	}

	db := &DB{
		storage: storage,
		index:   make(map[string]int64),
	}
	return db, nil
}

func (db *DB) Put(key string, value []byte) error {
	offset, err := db.storage.Append([]byte(key), value)
	if err != nil {
		return err
	}

	db.index[key] = offset
	return nil
}

func (db *DB) Get(key string) ([]byte, error) {
	offset, ok := db.index[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	value, err := db.storage.Read(offset)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (db *DB) Close() error {
	return db.storage.Close()
}
