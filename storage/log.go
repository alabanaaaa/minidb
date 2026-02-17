package storage

import (
	"encoding/binary"
	"os"
)

type EventLog struct {
	file *os.File
}

func OpenEventLog(path string) (*EventLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &EventLog{file: f}, nil
}

func (l *EventLog) Append(data []byte) error {
	length := uint32(len(data))

	err := binary.Write(l.file, binary.BigEndian, length)
	if err != nil {
		return err
	}

	_, err = l.file.Write(data)
	if err != nil {
		return err
	}

	return l.file.Sync()
}
