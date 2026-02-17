package engine

import "testing"

func TestInventoryAddReduce(t *testing.T) {
	inv := NewInventoryService()

	inv.Add("carrot", 10)

	err := inv.Reduce("carrot", 3)
	if err != nil {
		t.Fatal(err)
	}

	if inv.Get("carrot") != 7 {
		t.Errorf("expected 7 got %v", inv.Get("carrot"))
	}
}

func TestInventoryOversell(t *testing.T) {
	inv := NewInventoryService()

	inv.Add("carrot", 2)

	err := inv.Reduce("carrot", 5)
	if err == nil {
		t.Fatal("expected oversell error")
	}
}
