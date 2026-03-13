package ledger

import (
	"crypto/sha256"
)

func hashChain(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:] // Convert [32]byte array to []byte slice
}
