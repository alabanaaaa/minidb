package engine

import (
	"mini-database/core"
	"time"
)

func (e *Engine) Reconcile(workerID string, declaredCash, declaredMpesa int) core.Reconciliation {
	var expectedCash int
	var expectedMpesa int

	for _, log := range e.logs {
		sale, ok := log.(core.Sale)
		if !ok {
			continue
		}

		if sale.WorkerID != workerID {
			continue
		}

		total := int(float64(sale.Price) * sale.Quantity)

		if sale.Payment == "cash" {
			expectedCash += total
		}

		if sale.Payment == "mpesa" {
			expectedMpesa += total
		}
	}

	cashVariance := declaredCash - expectedCash
	mpesaVariance := declaredMpesa - expectedMpesa

	rec := core.Reconciliation{
		WorkerID:      workerID,
		ExpectedCash:  expectedCash,
		DeclaredCash:  declaredCash,
		ExpectedMpesa: expectedMpesa,
		DeclaredMpesa: declaredMpesa,
		CashVariance:  cashVariance,
		MpesaVariance: mpesaVariance,
		Timestamp:     time.Now(),
	}

	e.logs = append(e.logs, rec)

	return rec

}
