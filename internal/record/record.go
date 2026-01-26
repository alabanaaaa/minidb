package record

import (
	"encoding/binary"
	"io"
)

type Record struct {
	Key       []byte
	Value     []byte
	Tombstone bool
}

// Encode writes a record to a writer
func Encode(w io.Writer, key, value []byte, tombstone bool) (int64, error) {
	var written int64

	if err := binary.Write(w, binary.LittleEndian, uint32(len(key))); err != nil {
		return written, err
	}
	written += 4

	if err := binary.Write(w, binary.LittleEndian, uint32(len(value))); err != nil {
		return written, err
	}
	written += 4

	var tomb byte = 0
	if tombstone {
		tomb = 1
	}
	if _, err := w.Write([]byte{tomb}); err != nil {
		return written, err
	}
	written++

	n, err := w.Write(key)
	if err != nil {
		return written, err
	}
	written += int64(n)

	n, err = w.Write(value)
	if err != nil {
		return written, err
	}
	written += int64(n)

	return written, nil
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

	value := make([]byte, valueSize)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, 0, err
	}

	total := int64(4 + 4 + 1 + keySize + valueSize)

	return &Record{
		Key:       key,
		Value:     value,
		Tombstone: tomb[0] == 1,
	}, total, nil
}
