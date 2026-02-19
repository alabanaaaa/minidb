package core

import "time"

type PaymentMethod uint8

const (
	PaymentCash  PaymentMethod = 1
	PaymentMpesa PaymentMethod = 2
)

type Sale struct {
	ProductID string
	Quantity  int64
	Price     int64
	WorkerID  string
	Payment   PaymentMethod
	TimeStamp time.Time
}

// Validate checks if a Sale is valid
func (s *Sale) Validate() *DomainError {
	if s.ProductID == "" {
		return NewDomainError(ErrCodeInvalidSale, "product ID cannot be empty")
	}

	if s.Quantity <= 0 {
		return NewDomainError(ErrCodeNegativeValue, "quantity must be greater than zero")
	}

	if s.Price < 0 {
		return NewDomainError(ErrCodeNegativeValue, "price cannot be negative")
	}

	if s.WorkerID == "" {
		return NewDomainError(ErrCodeInvalidSale, "worker ID cannot be empty")
	}

	if s.Payment != PaymentCash && s.Payment != PaymentMpesa {
		return NewDomainError(ErrCodeInvalidSale, "invalid payment method")
	}

	return nil
}
