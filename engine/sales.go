package engine

import "time"

type Sale struct {
	ID          string
	ItemID      string
	Quantity    int
	Price       int
	WorkerID    string
	PaymentType string
	Timestamp   time.Time
}

type SalesService struct {
	sales []*Sale
}

func NewSalesService() *SalesService {
	return &SalesService{
		sales: []*Sale{},
	}
}

func (s *SalesService) RecordSale(sale Sale) {
	s.sales = append(s.sales, &sale)
}

func (s *SalesService) AllSales() []Sale {
	sales := make([]Sale, len(s.sales))
	for i, sale := range s.sales {
		sales[i] = *sale
	}
	return sales
}
