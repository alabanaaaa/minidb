package engine

import (
	"mini-database/core"
	"testing"
)

func TestStockIncrease(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	err := e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  1000, // 1 carrot = 1000 units
		Cost:      3000,
	})
	if err != nil {
		t.Fatalf("failed to apply stock: %v", err)
	}

	if e.GetStock("carrot") != 1000 {
		t.Errorf("expected stock 1000 got %v", e.GetStock("carrot"))
	}
}

func TestSaleReducesStock(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	err := e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  1000,
		Cost:      3000,
	})
	if err != nil {
		t.Fatalf("failed to apply stock: %v", err)
	}

	err = e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  200, // 0.2 carrot = 200 units
		Price:     100,
		WorkerID:  "worker1",
		Payment:   core.PaymentCash,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.GetStock("carrot") != 800 {
		t.Errorf("expected stock 800 got %v", e.GetStock("carrot"))
	}
}

func TestCannotOversell(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	err := e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  1000,
		Cost:      3000,
	})
	if err != nil {
		t.Fatalf("failed to apply stock: %v", err)
	}

	err = e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  2000,
		Price:     100,
		WorkerID:  "worker1",
		Payment:   core.PaymentCash,
	})

	if err == nil {
		t.Fatalf("expected error but got nil")
	}
}
