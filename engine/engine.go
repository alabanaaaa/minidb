package engine

import (
	"encoding/json"
	"fmt"
	"mini-database/core"
	coredb "mini-database/core/db"
	"sync"
)

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Engine struct {
	inventory *InventoryService
	db        *coredb.DB
	events    []Event // In-memory event log for reconciliation
	mu        sync.RWMutex
}

// In-memory only
func NewEngine() *Engine {
	return &Engine{
		inventory: NewInventoryService(),
		events:    []Event{},
	}
}

// Persistent engine
func NewEngineWithDB(dbPath string) (*Engine, error) {
	database, err := coredb.OpenDB(dbPath)
	if err != nil {
		return nil, core.NewDomainErrorWithCause(
			core.ErrCodePersistence,
			"failed to open database",
			err,
		)
	}

	engine := &Engine{
		inventory: NewInventoryService(),
		db:        database,
		events:    []Event{},
	}

	if err := engine.replay(); err != nil {
		database.Close()
		return nil, err
	}

	return engine, nil
}

func (e *Engine) replay() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.db == nil {
		return nil
	}

	data, err := e.db.Get("__event_log__")
	if err != nil {
		return nil // no events yet
	}

	var events []Event
	if err := json.Unmarshal([]byte(data), &events); err != nil {
		return core.NewDomainErrorWithCause(
			core.ErrCodePersistence,
			"failed to decode event log",
			err,
		)
	}

	for _, event := range events {
		if err := e.applyEvent(event); err != nil {
			return err
		}
	}

	e.events = events

	return nil
}

// Apply handles deterministic state transitions (used by replay AND runtime)
func (e *Engine) applyEvent(event Event) error {
	switch event.Type {

	case "stock":
		var stock core.StockItem
		if err := json.Unmarshal(event.Data, &stock); err != nil {
			return err
		}
		e.inventory.Add(stock.ProductID, stock.Quantity)

	case "sale":
		var sale core.Sale
		if err := json.Unmarshal(event.Data, &sale); err != nil {
			return err
		}

		if err := e.inventory.Reduce(sale.ProductID, sale.Quantity); err != nil {
			return err
		}

		current := e.inventory.Get(sale.ProductID)
		if current < sale.Quantity {
			return core.NewDomainError(
				core.ErrCodeInsufficientStock,
				fmt.Sprintf("insufficient stock during replay for %s", sale.ProductID),
			)
		}

		if err := e.inventory.Reduce(sale.ProductID, sale.Quantity); err != nil {
			return err
		}

	case "reconciliation":
		// For now reconciliation does not mutate inventory
		// Future: track worker balances
		return nil

	default:
		return core.NewDomainError(
			core.ErrCodeInvalidOperation,
			fmt.Sprintf("unknown event type: %s", event.Type),
		)
	}

	return nil
}

func (e *Engine) persist(event Event) error {
	// Always track events in memory for reconciliation
	e.events = append(e.events, event)

	if e.db == nil {
		return nil
	}

	existing, _ := e.db.Get("__event_log__")

	var events []Event
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &events); err != nil {
			return err
		}
	}

	events = append(events, event)

	updated, err := json.Marshal(events)
	if err != nil {
		return err
	}

	return e.db.Put("__event_log__", string(updated))
}

func (e *Engine) ApplyStock(stock core.StockItem) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := stock.Validate(); err != nil {
		return err
	}

	data, _ := json.Marshal(stock)

	event := Event{
		Type: "stock",
		Data: data,
	}

	if err := e.applyEvent(event); err != nil {
		return err
	}

	return e.persist(event)
}

func (e *Engine) ApplySale(sale core.Sale) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := sale.Validate(); err != nil {
		return err
	}

	data, _ := json.Marshal(sale)

	event := Event{
		Type: "sale",
		Data: data,
	}

	if err := e.applyEvent(event); err != nil {
		return err
	}

	return e.persist(event)
}

func (e *Engine) GetStock(productID string) int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.inventory.Get(productID)
}

func (e *Engine) Close() error {
	if e.db != nil {
		return e.db.Close()
	}
	return nil
}
