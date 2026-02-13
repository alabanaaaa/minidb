package engine

import (
	"mini-database/core"
	"testing"
)

func TestStockIncrease(t *testing.T) {
	e := NewEngine()

	e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  1.0,
		Cost:      3000,
	})

	if e.GetStock("carrot") != 1.0 {
		t.Errorf("expected stock 1.0 got %v", e.GetStock("carrot"))
	}
}

func TestSaleReducesStock(t *testing.T) {
	e := NewEngine()

	e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  1.0,
		Cost:      3000,
	})

	err := e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  0.2,
		Price:     100,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.GetStock("carrot") != 0.8 {
		t.Errorf("expected stock 0.8 got %v", e.GetStock("carrot"))
	}
}

func TestCannotOversell(t *testing.T) {
	e := NewEngine()

	e.ApplyStock(core.StockItem{
		ProductID: "carrot",
		Quantity:  1.0,
		Cost:      3000,
	})

	err := e.ApplySale(core.Sale{
		ProductID: "carrot",
		Quantity:  2.0,
		Price:     100,
	})

	if err == nil {
		t.Fatalf("expected error but got nil")
	}
}
