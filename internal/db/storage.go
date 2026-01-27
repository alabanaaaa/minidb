package minidatabase

import (
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

	_, err = record.Encode(
		s.file,
		key,
		value,
		tombstone,
	)

	if err != nil {
		return 0, err
	}

	if err := s.file.Sync(); err != nil {
		return 0, err
	}

	return offset, nil
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

func (s *Storage) Size() (int64, error) {
	info, err := s.file.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func (s *Storage) Close() error {
	return s.file.Close()
}
