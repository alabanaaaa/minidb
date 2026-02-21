package engine

import (
	"encoding/json"
	"mini-database/core"
	"time"
)

func (e *Engine) Reconcile(workerID string, declaredCash, declaredMpesa int64) (core.Reconciliation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var events []Event

	// Get events from persistent store or in-memory
	if e.db != nil {
		data, err := e.db.Get("__event_log__")
		if err != nil || data == "" {
			events = e.events
		} else {
			if err := json.Unmarshal([]byte(data), &events); err != nil {
				return core.Reconciliation{}, err
			}
		}
	} else {
		events = e.events
	}

	var expectedCash int64
	var expectedMpesa int64

	for _, event := range events {
		if event.Type != "sale" {
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

	if err := e.persist(event); err != nil {
		return rec, err
	}

	return rec, nil
}
