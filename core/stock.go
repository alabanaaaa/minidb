package core

type StockItem struct {
	ProductID string
	Quantity  int64
	Cost      int64
}

// Validate checks if a StockItem is valid
func (s *StockItem) Validate() *DomainError {
	if s.ProductID == "" {
		return NewDomainError(ErrCodeInvalidStock, "product ID cannot be empty")
	}

	if s.Quantity < 0 {
		return NewDomainError(ErrCodeNegativeValue, "quantity cannot be negative")
	}

	if s.Cost < 0 {
		return NewDomainError(ErrCodeNegativeValue, "cost cannot be negative")
	}

	return nil
}
