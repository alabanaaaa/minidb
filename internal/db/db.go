package minidatabase

import (
	"errors"
	"io"

	"os"
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

	// Replay existing data to build the in-memory index
	if err := db.Replay(); err != nil {
		return nil, err
	}

	return db, nil
}

/*
func (db *DB) Put(key string, value []byte) error {
	offset, err := db.storage.Append([]byte(key), value)
	if err != nil {
		return err
	}

	db.index[key] = offset
	return nil
}*/
// put API to insert key-value pairs into the database
func (db *DB) Put(key, value string) error {
	offset, err := db.storage.Append(
		[]byte(key),
		[]byte(value),
		false,
	)

	if err != nil {
		return err
	}

	db.index[key] = offset
	return nil
}

/*
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
}*/

//get API to retrieve value by key from the database

func (db *DB) Get(key string) (string, error) {
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

// Replay function to rebuild the in-memory index from the storage file
func (db *DB) Replay() error {
	var offset int64 = 0

	for {
		rec, n, err := db.storage.ReadAt(offset)
		if err != nil {
			if err == io.EOF {
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

// readNextRecord reads the next record from the storage file at the given offset
/*func (db *DB) readNextRecord(offset int64) ([]byte, int, error) {
	if _, err := db.storage.file.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err

	}

	var keySize uint32
	var valueSize uint32

	if err := binary.Read(db.storage.file, binary.LittleEndian, &keySize); err != nil {
		return nil, 0, err
	}

	if err := binary.Read(db.storage.file, binary.LittleEndian, &valueSize); err != nil {
		return nil, 0, err
	}

	// tombstone byte
	var tomb byte
	if _, err := io.ReadFull(db.storage.file, []byte{tomb}); err != nil {
		return nil, 0, err
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(db.storage.file, key); err != nil {
		return nil, 0, err
	}

	value := make([]byte, valueSize)
	if _, err := io.ReadFull(db.storage.file, value); err != nil {
		return nil, 0, err
	}

	totalBytes := 4 + 4 + 1 + int(keySize) + int(valueSize)
	return key, totalBytes, nil
} */

func (db *DB) Delete(key string) error {
	_, err := db.storage.Append(
		[]byte(key),
		nil,
		true, // tombstone flag set to true
	)
	if err != nil {
		return err
	}

	delete(db.index, key)
	return nil
}

func (db *DB) Compact() error {
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

		newOffset, err := newStorage.Append(
			rec.Key,
			rec.Value,
			false,
		)
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
func (db *DB) Close() error {
	return db.storage.Close()
}
