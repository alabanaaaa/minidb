package engine

type Policy struct {
	AllowDelete        bool
	AllowStockEdit     bool
	AllowPriceOverride bool
	RequireWorkerID    bool
}

func DefaultPolicy() Policy {
	return Policy{
		AllowDelete:        false,
		AllowStockEdit:     false,
		AllowPriceOverride: false,
		RequireWorkerID:    true,
	}
}
