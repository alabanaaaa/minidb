package engine

import (
	"encoding/json"
	"mini-database/core"
)

// SalesService no longer keeps an independent authoritative store.
// It can maintain a cache but must be populated from engine events.
type SalesService struct {
	sales []*core.Sale
}

func NewSalesService() *SalesService {
	return &SalesService{
		sales: []*core.Sale{},
	}
}

func (s *SalesService) RecordSale(engine *Engine, sale core.Sale) error {
	// Validate sale
	if err := sale.Validate(); err != nil {
		return err
	}
	// Record via Engine's event system to ensure consistency
	return engine.ApplySale(sale)
}

// PopulateFromEngine scans engine events and rebuilds the sales cache.
// Call this after engine replay or periodically to keep the cache consistent.
func (s *SalesService) PopulateFromEngine(engine *Engine) {
	s.sales = s.sales[:0]
	engine.mu.RLock()
	events := make([]Event, len(engine.events))
	copy(events, engine.events)
	engine.mu.RUnlock()

	for _, ev := range events {
		if ev.Type != "sale" {
			continue
		}
		var sale core.Sale
		_ = json.Unmarshal(ev.Data, &sale)
		// Only append valid unmarshaled sales
		if sale.ProductID != "" {
			salesCopy := sale
			s.sales = append(s.sales, &salesCopy)
		}
	}
}

func (s *SalesService) AllSales() []core.Sale {
	sales := make([]core.Sale, len(s.sales))
	for i, sale := range s.sales {
		sales[i] = *sale
	}
	return sales
}
