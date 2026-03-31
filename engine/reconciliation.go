package engine

import (
	"encoding/json"
	"mini-database/core"
	"time"
)

func (e *Engine) Reconcile(workerID string, declaredCash, declaredMpesa int64) (core.Reconciliation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	events := e.events

	var expectedCash int64
	var expectedMpesa int64

	var lastReconcileTime time.Time

	for _, event := range events {
		if event.Type != "reconciliation" {
			continue
		}

		var rec core.Reconciliation
		if err := json.Unmarshal(event.Data, &rec); err != nil {
			continue
		}

		if rec.WorkerID == workerID && event.Timestamp.After(lastReconcileTime) {
			lastReconcileTime = event.Timestamp
		}
	}

	for _, event := range events {
		if event.Type != "sale" {
			continue
		}

		if !event.Timestamp.After(lastReconcileTime) {
			continue
		}

		var sale core.Sale
		if err := json.Unmarshal(event.Data, &sale); err != nil {
			return core.Reconciliation{}, err
		}

		if sale.WorkerID != workerID {
			continue
		}
		total := sale.Price * sale.Quantity

		switch sale.Payment {
		case core.PaymentCash:
			expectedCash += total
		case core.PaymentMpesa:
			expectedMpesa += total
		}
	}

	rec := core.Reconciliation{
		WorkerID:      workerID,
		ExpectedCash:  int64(expectedCash),
		DeclaredCash:  int64(declaredCash),
		ExpectedMpesa: int64(expectedMpesa),
		DeclaredMpesa: int64(declaredMpesa),
		CashVariance:  int64(declaredCash - expectedCash),
		MpesaVariance: int64(declaredMpesa - expectedMpesa),
		Timestamp:     time.Now(),
	}

	if err := rec.Validate(); err != nil {
		return rec, err
	}

	recData, _ := json.Marshal(rec)

	event := Event{
		Type:      "reconciliation",
		Data:      recData,
		Timestamp: time.Now(),
	}

	if err := e.appendEventLocked(event); err != nil {
		return rec, err
	}

	return rec, nil
}
