package engine

import (
	"mini-database/core"
	"testing"
)

func TestLedgerIntegrity(t *testing.T) {

	e := NewEngine()
	defer e.Close()

	e.ApplyStock(core.StockItem{
		ProductID: "apple",
		Quantity:  100,
		Cost:      50,
	})

	e.ApplySale(core.Sale{
		ProductID: "apple",
		Quantity:  10,
		Price:     5,
		WorkerID:  "worker1",
		Payment:   core.PaymentCash,
	})

	err := e.VerifyLedger()

	if err != nil {
		t.Fatalf("ledger verification failed: %v", err)
	}
}
