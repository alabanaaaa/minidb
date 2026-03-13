package engine

import (
	"mini-database/core"
)

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

func (s *SalesService) AllSales() []core.Sale {
	sales := make([]core.Sale, len(s.sales))
	for i, sale := range s.sales {
		sales[i] = *sale
	}
	return sales
}
