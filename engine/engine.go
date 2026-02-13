package engine

import (
	"errors"
	"mini-database/core"
)

type Engine struct {
	inventory map[string]float64
	logs      []interface{}
}

func NewEngine() *Engine {
	return &Engine{
		inventory: make(map[string]float64),
		logs:      []interface{}{},
	}
}

func (e *Engine) ApplyStock(stock core.StockItem) {
	e.inventory[stock.ProductID] += stock.Quantity
	e.logs = append(e.logs, stock)
}

func (e *Engine) ApplySale(sale core.Sale) error {
	current := e.inventory[sale.ProductID]

	if current < sale.Quantity {
		return errors.New("insufficient stock")
	}

	e.inventory[sale.ProductID] -= sale.Quantity
	e.logs = append(e.logs, sale)

	return nil
}

func (e *Engine) GetStock(productID string) float64 {
	return e.inventory[productID]
}

func (e *Engine) Logs() []interface{} {
	return e.logs
}
