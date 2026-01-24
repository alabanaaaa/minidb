package record

import (
	"bytes"
	"encoding/binary"

	"io"
)

type Record struct {
	Key       []byte
	Value     []byte
	Tombstone bool
}

func Encode(r *Record) ([]byte, error) {
	buf := new(bytes.Buffer)
	// key size
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(r.Key))); err != nil {
		return nil, err
	}
	// value size
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(r.Value))); err != nil {
		return nil, err
	}

	//tombstone
	var tomb byte = 0
	if r.Tombstone {
		tomb = 1
	}

	if err := buf.WriteByte(tomb); err != nil {
		return nil, err
	}

	//key bytes
	if _, err := buf.Write(r.Key); err != nil {
		return nil, err
	}

	//value bytes
	if _, err := buf.Write(r.Value); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func Decode(r io.Reader) (*Record, int64, error) {
	var keySize uint32
	var valueSize uint32

	// read key size
	if err := binary.Read(r, binary.LittleEndian, &keySize); err != nil {
		return nil, 0, err
	}

	//read value size
	if err := binary.Read(r, binary.LittleEndian, &valueSize); err != nil {
		return nil, 0, err
	}

	//read tombstone
	tomb := make([]byte, 1)
	if _, err := r.Read(tomb); err != nil {
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

	record := &Record{
		Key:       key,
		Value:     value,
		Tombstone: tomb[0] == 1,
	}

	//total bytes read
	totalBytes := int64(4 + 4 + 1 + keySize + valueSize)

	return record, totalBytes, nil
}
