package core

type StockItem struct {
	ProductID string `json:"product_id"` // Make sure this tag exists
	Quantity  int64  `json:"quantity"`
	Cost      int64  `json:"cost"` // If you have a cost field
}

// Validate checks if a StockItem is valid
func (s StockItem) Validate() error {
	if s.ProductID == "" {
		return NewDomainError(ErrCodeInvalidStock, "product ID cannot be empty")
	}
	if s.Quantity <= 0 {
		return NewDomainError(ErrCodeInvalidStock, "quantity must be greater than 0")
	}
	if s.Cost < 0 {
		return NewDomainError(ErrCodeInvalidStock, "cost cannot be negative")
	}
	return nil
}
