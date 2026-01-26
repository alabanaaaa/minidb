package minidatabase

import (
	"encoding/binary"
	"errors"
	"io"
	"mini-database/internal/record"
	"os"
)

type Storage struct {
	file *os.File
	path string
}

func OpenStorage(path string) (*Storage, error) {
	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_RDWR,
		0644,
	)
	if err != nil {
		return nil, err
	}
	return &Storage{file: file, path: path}, nil
}

func (s *Storage) Append(key, value []byte, tombstone bool) (int64, error) {
	offset, err := s.file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	if err := binary.Write(s.file, binary.LittleEndian, uint32(len(key))); err != nil {
		return 0, err
	}

	if err := binary.Write(s.file, binary.LittleEndian, uint32(len(value))); err != nil {
		return 0, err

	}
	var tomb byte = 0
	if tombstone {
		tomb = 1
	}
	if _, err := s.file.Write([]byte{tomb}); err != nil {
		return 0, err
	}

	if _, err := s.file.Write(key); err != nil {
		return 0, err
	}

	if _, err := s.file.Write(value); err != nil {
		return 0, err
	}

	if err := s.file.Sync(); err != nil {
		return 0, err
	}

	return offset, nil

}

func (s *Storage) Read(offset int64) ([]byte, error) {
	if _, err := s.file.Seek(offset, 0); err != nil {
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

func (s *Storage) ReadAt(offset int64) (*record.Record, int64, error) {
	if _, err := s.file.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err
	}
	return record.Decode(s.file)
}

/*	// Read header first (keySize + valueSize + tombstone)
	header := make([]byte, 9) // 4 + 4 + 1
	if _, err := io.ReadFull(s.file, header); err != nil {
		return nil, err
	}

	keySize := binary.LittleEndian.Uint32(header[:4])
	valueSize := binary.LittleEndian.Uint32(header[4:8])

	totalBytes := 9 + int(keySize) + int(valueSize)

	data := make([]byte, totalBytes)
	copy(data[:9], header)

	if _, err := io.ReadFull(s.file, data[9:]); err != nil {
		return nil, err
	}

	return data, nil
*/

func (s *Storage) Close() error {
	return s.file.Close()
}
