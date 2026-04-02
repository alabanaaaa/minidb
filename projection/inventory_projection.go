package projection

import (
	"encoding/json"
)

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

func (p *InventoryProjection) Handle(event Event) error {
	return p.Apply(event)
}

func (p *InventoryProjection) Apply(event Event) error {
	switch event.Type {
	case "stock":
		var payload struct {
			ProductID string `json:"product_id"`
			Quantity  int    `json:"quantity"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		p.stock[payload.ProductID] += payload.Quantity

	case "sale":
		var payload struct {
			ProductID string `json:"product_id"`
			Quantity  int    `json:"quantity"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		p.stock[payload.ProductID] -= payload.Quantity
	}

	return nil
}

func (p *InventoryProjection) GetStock(product string) int {
	return p.stock[product]
}
