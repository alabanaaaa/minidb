package core

import "time"

type Sale struct {
	ProductID string
	Quantity  float64
	Price     int
	WorkerID  string
	Payment   string
	TimeStamp time.Time
}
