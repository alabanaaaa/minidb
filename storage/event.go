package storage

import (
	"crypto/sha256"
	"encoding/binary"
)

type EventType uint8

const (
	EventStock          EventType = 1
	EventSale           EventType = 2
	EventReconciliation EventType = 3
)

type Event struct {
	Version      uint8
	Type         EventType
	Timestamp    int64
	Payload      []byte
	PreviousHash [32]byte
	Hash         [32]byte
}

func (e *Event) ComputeHash() {
	buf := make([]byte, 0)

	buf = append(buf, e.Version)
	buf = append(buf, byte(e.Type))

	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(e.Timestamp))
	buf = append(buf, ts...)

	buf = append(buf, e.Payload...)
	buf = append(buf, e.PreviousHash[:]...)

	e.Hash = sha256.Sum256(buf)
}
