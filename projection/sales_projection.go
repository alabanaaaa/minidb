package projection

import (
	"encoding/json"
	db "mini-database/core/db"
	"mini-database/storage"
)

type SalesProjection struct {
	db *db.DB
}

func NewSalesProjection(database *db.DB) *SalesProjection {
	return &SalesProjection{
		db: database,
	}
}

func (p *SalesProjection) Handle(evt storage.Event) error {
	if evt.Type != storage.EventSale {
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

	return p.db.Exec(
		`INSERT INTO sales_view(product_id, quantity, price, worker_id, total, payment_method) VALUES (?, ?, ?, ?, ?, ?)`,
		sale.ProductID,
		sale.Quantity,
		sale.Price,
		sale.WorkerID,
		total,
		sale.PaymentMethod,
	)
}
