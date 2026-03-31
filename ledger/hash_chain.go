package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Event struct {
	ID           string      `json:"id"`
	Data         interface{} `json:"data"`
	Hash         string      `json:"hash"`
	PreviousHash string      `json:"previous_hash"`
}

// ComputeHashFromBytes computes SHA256(hex) from raw data bytes + previous hash
func ComputeHashFromBytes(data []byte, previousHash string) string {
	// ensure deterministic input: use the raw bytes (assumed to be JSON) + previousHash
	content := string(data) + previousHash
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// ComputeHash serializes event.Data then delegates to ComputeHashFromBytes
func ComputeHash(event Event) string {
	dataBytes, _ := json.Marshal(event.Data)
	return ComputeHashFromBytes(dataBytes, event.PreviousHash)
}
