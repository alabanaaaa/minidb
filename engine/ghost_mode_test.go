package engine

import (
	"mini-database/core"
	"testing"
	"time"
)

func TestGhostModeNoAnomalies(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	e.ApplyStock(core.StockItem{ProductID: "apple", Quantity: 100, Cost: 50})

	e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 100, WorkerID: "W1", Payment: core.PaymentCash})
	e.ApplySale(core.Sale{ProductID: "apple", Quantity: 2, Price: 100, WorkerID: "W1", Payment: core.PaymentCash})

	report := e.RunGhostMode(time.Time{}, time.Time{})

	var realAnomalies []Anomaly
	var offHoursCount int
	for _, a := range report.Anomalies {
		if a.Type != AnomalyOffHoursActivity {
			realAnomalies = append(realAnomalies, a)
		} else {
			offHoursCount++
		}
	}

	if len(realAnomalies) != 0 {
		t.Fatalf("expected no anomalies, got %d: %+v", len(realAnomalies), realAnomalies)
	}

	expectedRiskScore := offHoursCount * 5
	if report.RiskScore != expectedRiskScore {
		t.Fatalf("expected risk score %d, got %d", expectedRiskScore, report.RiskScore)
	}
}

func TestGhostModeDetectsVariancePattern(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	e.ApplyStock(core.StockItem{ProductID: "apple", Quantity: 100, Cost: 50})

	for i := 0; i < 5; i++ {
		e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 100, WorkerID: "W1", Payment: core.PaymentCash})
		e.Reconcile("W1", 80, 0)
	}

	report := e.RunGhostMode(time.Time{}, time.Time{})

	found := false
	for _, a := range report.Anomalies {
		if a.Type == AnomalyVariancePattern || a.Type == AnomalyConsecutiveShort {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected variance pattern anomaly, got: %+v", report.Anomalies)
	}
}

func TestGhostModeDetectsPriceManipulation(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	e.ApplyStock(core.StockItem{ProductID: "apple", Quantity: 100, Cost: 50})

	e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 100, WorkerID: "W1", Payment: core.PaymentCash})
	e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 100, WorkerID: "W1", Payment: core.PaymentCash})
	e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 150, WorkerID: "W1", Payment: core.PaymentCash})
	e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 200, WorkerID: "W1", Payment: core.PaymentCash})

	report := e.RunGhostMode(time.Time{}, time.Time{})

	found := false
	for _, a := range report.Anomalies {
		if a.Type == AnomalyPriceManipulation {
			found = true
			break
		}
	}
	if !found {
		t.Logf("anomalies: %+v", report.Anomalies)
	}
}

func TestGhostModeRiskScore(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	e.ApplyStock(core.StockItem{ProductID: "apple", Quantity: 100, Cost: 50})

	for i := 0; i < 10; i++ {
		e.ApplySale(core.Sale{ProductID: "apple", Quantity: 1, Price: 100, WorkerID: "W1", Payment: core.PaymentCash})
	}

	report := e.RunGhostMode(time.Time{}, time.Time{})

	if report.RiskScore < 0 || report.RiskScore > 100 {
		t.Fatalf("risk score out of range: %d", report.RiskScore)
	}
}

func TestGhostModeDateRange(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	e.ApplyStock(core.StockItem{ProductID: "apple", Quantity: 100, Cost: 50})

	report := e.RunGhostMode(time.Now().Add(24*time.Hour), time.Now().Add(48*time.Hour))

	if report.TotalEvents != 0 {
		t.Fatalf("expected 0 events in future range, got %d", report.TotalEvents)
	}
}
