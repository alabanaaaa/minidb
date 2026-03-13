package projection

import (
	"mini-database/storage"
)

type InventoryProjection struct{}

func NewInventoryProjection() *InventoryProjection {
	return &InventoryProjection{}
}

func (p *InventoryProjection) Handle(evt storage.Event) error {

	return nil
}
