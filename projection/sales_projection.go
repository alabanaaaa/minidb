package projection

import (
	"encoding/json"
)

type SalesProjection struct {
	sales []SaleRecord
}

type SaleRecord struct {
	ProductID     string `json:"product_id"`
	Quantity      int64  `json:"quantity"`
	Price         int64  `json:"price"`
	WorkerID      string `json:"worker_id"`
	PaymentMethod uint8  `json:"payment_method"`
	Total         int64  `json:"total"`
}

func NewSalesProjection() *SalesProjection {
	return &SalesProjection{
		sales: []SaleRecord{},
	}
}

func (p *SalesProjection) Name() string {
	return "sales"
}

func (p *SalesProjection) Handle(evt Event) error {
	return p.Apply(evt)
}

func (p *SalesProjection) Apply(evt Event) error {
	if evt.Type != "sale" {
		return nil
	}

	var sale struct {
		ProductID     string `json:"product_id"`
		Quantity      int64  `json:"quantity"`
		Price         int64  `json:"price"`
		WorkerID      string `json:"worker_id"`
		PaymentMethod uint8  `json:"payment"`
	}

	if err := json.Unmarshal(evt.Payload, &sale); err != nil {
		return err
	}

	total := sale.Price * sale.Quantity
	p.sales = append(p.sales, SaleRecord{
		ProductID:     sale.ProductID,
		Quantity:      sale.Quantity,
		Price:         sale.Price,
		WorkerID:      sale.WorkerID,
		PaymentMethod: sale.PaymentMethod,
		Total:         total,
	})

	return nil
}

func (p *SalesProjection) Sales() []SaleRecord {
	return p.sales
}
