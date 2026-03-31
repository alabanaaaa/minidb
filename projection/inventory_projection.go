package projection

import "mini-database/storage"

type InventoryProjection struct {
	stock map[string]int
}

func NewInventoryProjection() *InventoryProjection {
	return &InventoryProjection{
		stock: map[string]int{},
	}
}

func (p *InventoryProjection) Name() string {
	return "inventory"
}

func (p *InventoryProjection) Apply(event storage.Event) error {

	switch event.Type {

	case storage.EventStock:
		// decode payload and add stock

	case storage.EventSale:
		// decode payload and reduce stock

	}

	return nil
}

func (p *InventoryProjection) GetStock(product string) int {
	return p.stock[product]
}
