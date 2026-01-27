package record

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

type Record struct {
	Key       []byte
	Value     []byte
	Tombstone bool
	Checksum  uint32
}

// Encode writes a record to a writer
func Encode(w io.Writer, key, value []byte, tombstone bool) (int64, error) {

	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.LittleEndian, uint32(len(key))); err != nil {
		return 0, err
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(value))); err != nil {
		return 0, err
	}

	var tomb byte = 0
	if tombstone {
		tomb = 1
	}

	buf.WriteByte(tomb)
	buf.Write(key)
	buf.Write(value)

	checksum := crc32.Checksum(buf.Bytes(), crc32.MakeTable(crc32.Castagnoli))
	if err := binary.Write(buf, binary.LittleEndian, checksum); err != nil {
		return 0, err
	}

	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

// Decode reads a record from a reader
func Decode(r io.Reader) (*Record, int64, error) {
	var keySize uint32
	var valueSize uint32

	if err := binary.Read(r, binary.LittleEndian, &keySize); err != nil {
		return nil, 0, err
	}

	if err := binary.Read(r, binary.LittleEndian, &valueSize); err != nil {
		return nil, 0, err
	}

	tomb := make([]byte, 1)
	if _, err := io.ReadFull(r, tomb); err != nil {
		return nil, 0, err
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, 0, err
	}

	// For tombstones, valueSize is 0 and should not have value data
	// For regular records, read the value data
	var value []byte
	if valueSize > 0 {
		value = make([]byte, valueSize)
		if _, err := io.ReadFull(r, value); err != nil {
			return nil, 0, err
		}
	}

	var storedChecksum uint32
	if err := binary.Read(r, binary.LittleEndian, &storedChecksum); err != nil {
		return nil, 0, err
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, keySize)
	binary.Write(buf, binary.LittleEndian, valueSize)
	buf.Write(tomb)
	buf.Write(key)
	buf.Write(value)

	computed := crc32.Checksum(buf.Bytes(), crc32.MakeTable(crc32.Castagnoli))
	if computed != storedChecksum {
		return nil, 0, errors.New("checksum mismatch(corrupted record)")
	}

	// total bytes read: keySize(4) + valueSize(4) + tombstone(1) + key + value + checksum(4)
	total := int64(4 + 4 + 1 + keySize + valueSize + 4)

	return &Record{
		Key:       key,
		Value:     value,
		Tombstone: tomb[0] == 1,
	}, total, nil
}
