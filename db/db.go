package minidatabase

import (
	"encoding/binary"
	"fmt"
	"io"
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

// Replay function to rebuild the in-memory index from the storage file
func (db *DB) Replay() error {
	var offset int64 = 0

	if _, err := db.storage.file.Seek(0, 0); err != nil {
		return err
	}

	for {
		key, _, n, err := db.readNextRecord(offset)
		if err != nil {
			//EOF is expected at the end of file
			if err == io.EOF {
				break
			}
			return err
		}
		// update in-memory index to latest offset
		db.index[string(key)] = offset

		// move offset to the next record
		offset += int64(n)
	}

	return nil
}

// readNextRecord reads the next record from the storage file at the given offset
func (db *DB) readNextRecord(offset int64) ([]byte, []byte, int, error) {
	if _, err := db.storage.file.Seek(offset, 0); err != nil {
		return nil, nil, 0, err
	}

	var keySize uint32
	var valueSize uint32

	if err := binary.Read(file, binary.LittleEndian, &keySize); err != nil {
		return nil, nil, 0, err
	}

	if err := binary.Read(db.storage.file, binary.LittleEndian, &valueSize); err != nil {
		return nil, nil, 0, err
	}

	key := make([]byte, keySize)
	n1, err := db.storage.file.Read(key)
	if err != nil {
		return nil, nil, 0, err
	}

	value := make([]byte, valueSize)
	n2, err := db.storage.file.Read(value)
	if err != nil {
		return nil, nil, 0, err
	}

	totalBytes := 4 + 4 + n1 + n2 // keySize(4 bytes) + valueSize(4 bytes) + key + value
	return key, value, totalBytes, nil

}
