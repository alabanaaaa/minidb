package engine

import "time"

type StockItem struct {
	ID        string
	Name      string
	CostPrice int
	Quantity  int
	CreatedAt time.Time
}

type InventoryService struct {
	items map[string]*StockItem
}

func NewInventoryService() *InventoryService {
	return &InventoryService{
		items: make(map[string]*StockItem),
	}
}

func (i *InventoryService) AddItem(id, name string, costPrice, quantity int) error {
	i.items[id] = &StockItem{
		ID:        id,
		Name:      name,
		CostPrice: costPrice,
		Quantity:  quantity,
		CreatedAt: time.Now(),
	}
	return nil

}

func (i *InventoryService) ReducedStock(id string, quantity int) bool {
	item, exists := i.items[id]
	if !exists || item.Quantity < quantity {
		return false
	}
	item.Quantity -= quantity
	return true
}

func (i *InventoryService) GetStock(id string) (*StockItem, bool) {
	item, exists := i.items[id]
	return item, exists
}
