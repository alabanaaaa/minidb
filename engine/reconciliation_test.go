package engine

import (
	"mini-database/core"
	"testing"
	"time"
)

func TestReconciliation(t *testing.T) {
	e := NewEngine()

	// Add stock first
	e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  10,
		Cost:      3000,
	})

	// Record sales
	e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  1,
		Price:     100,
		WorkerID:  "worker1",
		Payment:   "cash",
		TimeStamp: time.Now(),
	})

	e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  2,
		Price:     80,
		WorkerID:  "worker1",
		Payment:   "mpesa",
		TimeStamp: time.Now(),
	})

	rec := e.Reconcile("worker1", 100, 160)

	if rec.CashVariance != 0 {
		t.Errorf("expected cash variance 0, got %d", rec.CashVariance)
	}

	if rec.MpesaVariance != 0 {
		t.Errorf("expected mpesa variance 0, got %d", rec.MpesaVariance)
	}
}
