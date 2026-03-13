package projection

import (
	"encoding/json"
	db "mini-database/core/db"
	"mini-database/storage"
)

type SalePayload struct {
	StoreID string
	SaleID  string
	Total   int64
}

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

	var sale SalePayload

	if err := json.Unmarshal(evt.Payload, &sale); err != nil {
		return err
	}

	return p.db.Exec(
		`INSERT INTO sales_view(store_id, sale_id, total) VALUES (?, ?, ?)`,
		sale.StoreID,
		sale.SaleID,
		sale.Total,
	)
}
