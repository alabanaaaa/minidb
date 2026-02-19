package core

import "time"

type Reconciliation struct {
	WorkerID      string
	ExpectedCash  int64
	DeclaredCash  int64
	ExpectedMpesa int64
	DeclaredMpesa int64
	CashVariance  int64
	MpesaVariance int64
	Timestamp     time.Time
}

// Validate checks if a Reconciliation is valid
func (r *Reconciliation) Validate() *DomainError {
	if r.WorkerID == "" {
		return NewDomainError(ErrCodeInvalidReconcile, "worker ID cannot be empty")
	}

	if r.DeclaredCash < 0 {
		return NewDomainError(ErrCodeNegativeValue, "declared cash cannot be negative")
	}

	if r.DeclaredMpesa < 0 {
		return NewDomainError(ErrCodeNegativeValue, "declared mpesa cannot be negative")
	}

	return nil
}
