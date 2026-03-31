package engine

import (
	"mini-database/core"
	"testing"
)

func TestSalesServicePopulateFromEngine(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	svc := NewSalesService()

	// seed stock
	if err := e.ApplyStock(core.StockItem{ProductID: "apple", Quantity: 100}); err != nil {
		t.Fatalf("ApplyStock failed: %v", err)
	}

	// record two sales
	if err := e.ApplySale(core.Sale{ProductID: "apple", Quantity: 2, Price: 5, WorkerID: "w1", Payment: core.PaymentCash}); err != nil {
		t.Fatalf("ApplySale1 failed: %v", err)
	}
	if err := e.ApplySale(core.Sale{ProductID: "apple", Quantity: 3, Price: 5, WorkerID: "w2", Payment: core.PaymentMpesa}); err != nil {
		t.Fatalf("ApplySale2 failed: %v", err)
	}

	// populate cache from engine events
	svc.PopulateFromEngine(e)
	all := svc.AllSales()
	if len(all) != 2 {
		t.Fatalf("expected 2 sales in cache, got %d", len(all))
	}

	// quick content check
	if all[0].ProductID == "" || all[1].ProductID == "" {
		t.Fatal("populated sales missing ProductID")
	}
}
