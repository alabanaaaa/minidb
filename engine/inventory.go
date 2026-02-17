package engine

import (
	"errors"
	"sync"
)

type InventoryService struct {
	mu    sync.RWMutex
	items map[string]float64
}

func NewInventoryService() *InventoryService {
	return &InventoryService{
		items: make(map[string]float64),
	}
}

func (i *InventoryService) Add(productID string, quantity float64) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.items[productID] += quantity
}

func (i *InventoryService) Reduce(productID string, quantity float64) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	current := i.items[productID]

	if current < quantity {
		return errors.New("insufficient stock")
	}

	i.items[productID] -= quantity
	return nil
}

func (i *InventoryService) Get(productID string) float64 {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.items[productID]
}
