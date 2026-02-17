package engine

import (
	"errors"
	"mini-database/core"
)

type Engine struct {
	inventory *InventoryService
	logs      []any
}

func NewEngine() *Engine {
	return &Engine{
		inventory: NewInventoryService(),
		logs:      []any{},
	}
}

func (e *Engine) ApplyStock(stock core.StockItem) {
	e.inventory.Add(stock.ProductID, stock.Quantity)
	e.logs = append(e.logs, stock)
}

func (e *Engine) ApplySale(sale core.Sale) error {
	current := e.inventory.Get(sale.ProductID)

	if current < sale.Quantity {
		return errors.New("insufficient stock")
	}

	e.inventory.Reduce(sale.ProductID, sale.Quantity)
	e.logs = append(e.logs, sale)

	return nil
}

func (e *Engine) GetStock(productID string) float64 {
	return e.inventory.Get(productID)
}
