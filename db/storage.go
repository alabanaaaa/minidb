package minidatabase

import (
	"encoding/binary"
	"errors"
	"os"
)

type Storage struct {
	file *os.File
}

func OpenStorage(path string) (*Storage, error) {
	file, err := os.Open(
		path,
		os.O_CREATE|os.O_RDWR,
		0644,
	)
	if err != nil {
		return nil, err
	}
	return &Storage{file: file}, nil
}

func (s *Storage) Append(key, value []byte) (int64, error) {
	//move to end of file
	offset, err := s.file.Seek(0, os.SEEK_END)
	if err != nil {
		return 0, err
	}

	//write key size
	if err := binary.Write(s.file, binary.LittleEndian, uint32(len(key))); err != nil {
		return 0, err
	}

	// write value size
	if err := binary.Write(s.file, binary.LittleEndian, uint32(len(value))); err != nil {
		return 0, err
	}

	// Write key and value
	if _, err := s.file.Write(key); err != nil {
		return 0, err
	}

	// Ensure data is flushed to disk
	if err := s.file.Sync(); err != nil {
		return 0, err
	}
	return offset, nil
}

func (s *Storage) Read(offset int64) ([]byte, error) {
	if _, err := s.file.Seek(offset, os.SEEK_SET); err != nil {
		return nil, err
	}

	var keySize uint32
	var valueSize uint32

	if err := binary.Read(s.file, binary.LittleEndian, &keySize); err != nil {
		return nil, err
	}

	if err := binary.Read(s.file, binary.LittleEndian, &valueSize); err != nil {
		return nil, err
	}

	//skip key

	if _, err := s.file.Seek(int64(keySize), os.SEEK_CUR); err != nil {
		return nil, err
	}

	value := make([]byte, valueSize)
	n, err := s.file.Read(value)
	if err != nil {
		return nil, err
	}

	if uint32(n) != valueSize {
		return nil, errors.New("incomplete read")
	}
	return value, nil
}

func (s *Storage) Close() error {
	return s.file.Close()
}
