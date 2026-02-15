package core

import "time"

type Reconciliation struct {
	WorkerID      string
	ExpectedCash  int
	DeclaredCash  int
	ExpectedMpesa int
	DeclaredMpesa int
	CashVariance  int
	MpesaVariance int
	Timestamp     time.Time
}
