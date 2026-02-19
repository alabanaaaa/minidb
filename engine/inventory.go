package engine

import (
	"mini-database/core"
)

type InventoryService struct {
	items map[string]int64
}

func NewInventoryService() *InventoryService {
	return &InventoryService{
		items: make(map[string]int64),
	}
}

func (i *InventoryService) Add(productID string, qty int64) {
	i.items[productID] += qty
}

func (i *InventoryService) Reduce(productID string, qty int64) error {
	current := i.items[productID]

	if current < qty {
		return core.NewDomainError(
			core.ErrCodeInsufficientStock,
			"insufficient stock",
		)
	}

	i.items[productID] -= qty
	return nil
}

func (i *InventoryService) Get(productID string) int64 {
	return i.items[productID]
}
