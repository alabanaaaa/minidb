package engine

import (
	"mini-database/core"
	"testing"
	"time"
)

func TestReconciliation(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	// Add stock first
	err := e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  10,
		Cost:      3000,
	})
	if err != nil {
		t.Fatalf("failed to apply stock: %v", err)
	}

	// Record sales
	err = e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  1,
		Price:     100,
		WorkerID:  "worker1",
		Payment:   core.PaymentCash,
		TimeStamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to apply sale: %v", err)
	}

	err = e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  2,
		Price:     80,
		WorkerID:  "worker1",
		Payment:   core.PaymentMpesa,
		TimeStamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to apply second sale: %v", err)
	}

	rec, err := e.Reconcile("worker1", 100, 160)
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	if rec.CashVariance != 0 {
		t.Errorf("expected cash variance 0, got %d", rec.CashVariance)
	}

	if rec.MpesaVariance != 0 {
		t.Errorf("expected mpesa variance 0, got %d", rec.MpesaVariance)
	}
}
