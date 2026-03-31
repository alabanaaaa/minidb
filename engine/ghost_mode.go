package engine

import (
	"encoding/json"
	"fmt"
	"mini-database/core"
	"time"
)

// GhostMode runs anomaly detection on the event ledger to flag suspicious patterns.

type AnomalyType string

const (
	AnomalyVariancePattern   AnomalyType = "variance_pattern"
	AnomalyPriceManipulation AnomalyType = "price_manipulation"
	AnomalyOffHoursActivity  AnomalyType = "off_hours_activity"
	AnomalyStockDrift        AnomalyType = "stock_drift"
	AnomalyConsecutiveShort  AnomalyType = "consecutive_short"
)

type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type Anomaly struct {
	Type        AnomalyType `json:"type"`
	Severity    Severity    `json:"severity"`
	WorkerID    string      `json:"worker_id,omitempty"`
	ProductID   string      `json:"product_id,omitempty"`
	Description string      `json:"description"`
	Details     string      `json:"details"`
	DetectedAt  time.Time   `json:"detected_at"`
	FirstSeen   time.Time   `json:"first_seen,omitempty"`
	LastSeen    time.Time   `json:"last_seen,omitempty"`
	Count       int         `json:"count,omitempty"`
	Amount      int64       `json:"amount,omitempty"`
}

type GhostReport struct {
	ShopID      string    `json:"shop_id"`
	GeneratedAt time.Time `json:"generated_at"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	TotalEvents int       `json:"total_events"`
	Anomalies   []Anomaly `json:"anomalies"`
	Summary     string    `json:"summary"`
	RiskScore   int       `json:"risk_score"` // 0-100, higher = more risk
}

func (e *Engine) RunGhostMode(from, to time.Time) GhostReport {
	e.mu.RLock()
	events := make([]Event, len(e.events))
	copy(events, e.events)
	e.mu.RUnlock()

	report := GhostReport{
		GeneratedAt: time.Now(),
		PeriodStart: from,
		PeriodEnd:   to,
		TotalEvents: len(events),
	}

	var filtered []Event
	for _, ev := range events {
		if !from.IsZero() && ev.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && ev.Timestamp.After(to) {
			continue
		}
		filtered = append(filtered, ev)
	}

	report.TotalEvents = len(filtered)

	report.Anomalies = append(report.Anomalies, e.detectVariancePatterns(filtered)...)
	report.Anomalies = append(report.Anomalies, e.detectConsecutiveShort(filtered)...)
	report.Anomalies = append(report.Anomalies, e.detectPriceManipulation(filtered)...)
	report.Anomalies = append(report.Anomalies, e.detectOffHoursActivity(filtered)...)
	report.Anomalies = append(report.Anomalies, e.detectStockDrift(filtered)...)

	report.RiskScore = calculateRiskScore(report.Anomalies)
	report.Summary = generateSummary(report)

	return report
}

func (e *Engine) detectVariancePatterns(events []Event) []Anomaly {
	var anomalies []Anomaly

	workerReconciliations := make(map[string][]core.Reconciliation)

	for _, ev := range events {
		if ev.Type != "reconciliation" {
			continue
		}
		var rec core.Reconciliation
		if err := json.Unmarshal(ev.Data, &rec); err != nil {
			continue
		}
		workerReconciliations[rec.WorkerID] = append(workerReconciliations[rec.WorkerID], rec)
	}

	for workerID, recs := range workerReconciliations {
		if len(recs) < 2 {
			continue
		}

		shortCount := 0
		totalVariance := int64(0)
		firstSeen := recs[0].Timestamp
		lastSeen := recs[len(recs)-1].Timestamp

		for _, rec := range recs {
			cashVar := rec.CashVariance
			mpesaVar := rec.MpesaVariance
			if cashVar < 0 || mpesaVar < 0 {
				shortCount++
			}
			totalVariance += cashVar + mpesaVar
		}

		shortRate := float64(shortCount) / float64(len(recs))

		if shortRate >= 0.5 {
			severity := SeverityMedium
			if shortRate >= 0.8 {
				severity = SeverityHigh
			}
			if shortRate >= 0.95 && len(recs) >= 5 {
				severity = SeverityCritical
			}

			anomalies = append(anomalies, Anomaly{
				Type:        AnomalyVariancePattern,
				Severity:    severity,
				WorkerID:    workerID,
				Description: fmt.Sprintf("Worker %s has negative variance in %d/%d reconciliations (%.0f%%)", workerID, shortCount, len(recs), shortRate*100),
				Details:     fmt.Sprintf("Total cumulative variance: %d across %d reconciliations", totalVariance, len(recs)),
				DetectedAt:  time.Now(),
				FirstSeen:   firstSeen,
				LastSeen:    lastSeen,
				Count:       shortCount,
				Amount:      totalVariance,
			})
		}
	}

	return anomalies
}

func (e *Engine) detectConsecutiveShort(events []Event) []Anomaly {
	var anomalies []Anomaly

	workerReconciliations := make(map[string][]core.Reconciliation)

	for _, ev := range events {
		if ev.Type != "reconciliation" {
			continue
		}
		var rec core.Reconciliation
		if err := json.Unmarshal(ev.Data, &rec); err != nil {
			continue
		}
		workerReconciliations[rec.WorkerID] = append(workerReconciliations[rec.WorkerID], rec)
	}

	for workerID, recs := range workerReconciliations {
		if len(recs) < 3 {
			continue
		}

		maxConsecutive := 0
		currentConsecutive := 0
		consecutiveTotal := int64(0)
		consecutiveStart := time.Time{}

		for _, rec := range recs {
			if rec.CashVariance < 0 || rec.MpesaVariance < 0 {
				if currentConsecutive == 0 {
					consecutiveStart = rec.Timestamp
				}
				currentConsecutive++
				consecutiveTotal += rec.CashVariance + rec.MpesaVariance
			} else {
				if currentConsecutive > maxConsecutive {
					maxConsecutive = currentConsecutive
				}
				currentConsecutive = 0
				consecutiveTotal = 0
			}
		}
		if currentConsecutive > maxConsecutive {
			maxConsecutive = currentConsecutive
		}

		if maxConsecutive >= 3 {
			severity := SeverityMedium
			if maxConsecutive >= 5 {
				severity = SeverityHigh
			}
			if maxConsecutive >= 7 {
				severity = SeverityCritical
			}

			anomalies = append(anomalies, Anomaly{
				Type:        AnomalyConsecutiveShort,
				Severity:    severity,
				WorkerID:    workerID,
				Description: fmt.Sprintf("Worker %s was short for %d consecutive reconciliations", workerID, maxConsecutive),
				Details:     fmt.Sprintf("Total missing during streak: %d", consecutiveTotal),
				DetectedAt:  time.Now(),
				FirstSeen:   consecutiveStart,
				Count:       maxConsecutive,
				Amount:      consecutiveTotal,
			})
		}
	}

	return anomalies
}

func (e *Engine) detectPriceManipulation(events []Event) []Anomaly {
	var anomalies []Anomaly

	productSales := make(map[string]map[string][]saleRecord)

	for _, ev := range events {
		if ev.Type != "sale" {
			continue
		}
		var sale core.Sale
		if err := json.Unmarshal(ev.Data, &sale); err != nil {
			continue
		}

		if productSales[sale.ProductID] == nil {
			productSales[sale.ProductID] = make(map[string][]saleRecord)
		}
		productSales[sale.ProductID][sale.WorkerID] = append(productSales[sale.ProductID][sale.WorkerID], saleRecord{
			Price:     sale.Price,
			WorkerID:  sale.WorkerID,
			Timestamp: ev.Timestamp,
		})
	}

	for productID, workers := range productSales {
		for workerID, sales := range workers {
			if len(sales) < 3 {
				continue
			}

			minPrice := sales[0].Price
			maxPrice := sales[0].Price
			for _, s := range sales {
				if s.Price < minPrice {
					minPrice = s.Price
				}
				if s.Price > maxPrice {
					maxPrice = s.Price
				}
			}

			if maxPrice > 0 && minPrice > 0 {
				priceDiff := float64(maxPrice-minPrice) / float64(minPrice)
				if priceDiff > 0.2 {
					severity := SeverityLow
					if priceDiff > 0.5 {
						severity = SeverityMedium
					}
					if priceDiff > 1.0 {
						severity = SeverityHigh
					}

					anomalies = append(anomalies, Anomaly{
						Type:        AnomalyPriceManipulation,
						Severity:    severity,
						WorkerID:    workerID,
						ProductID:   productID,
						Description: fmt.Sprintf("Worker %s sold %s at varying prices (min: %d, max: %d, spread: %.0f%%)", workerID, productID, minPrice, maxPrice, priceDiff*100),
						Details:     fmt.Sprintf("Based on %d sales", len(sales)),
						DetectedAt:  time.Now(),
						Count:       len(sales),
					})
				}
			}
		}
	}

	return anomalies
}

func (e *Engine) detectOffHoursActivity(events []Event) []Anomaly {
	var anomalies []Anomaly

	shopHours := struct {
		Open  int
		Close int
	}{
		Open:  6,
		Close: 22,
	}

	for _, ev := range events {
		if ev.Type != "sale" {
			continue
		}
		hour := ev.Timestamp.Hour()
		if hour < shopHours.Open || hour >= shopHours.Close {
			var sale core.Sale
			if err := json.Unmarshal(ev.Data, &sale); err != nil {
				continue
			}

			anomalies = append(anomalies, Anomaly{
				Type:        AnomalyOffHoursActivity,
				Severity:    SeverityLow,
				WorkerID:    sale.WorkerID,
				Description: fmt.Sprintf("Sale recorded outside business hours at %s by worker %s", ev.Timestamp.Format("15:04"), sale.WorkerID),
				Details:     fmt.Sprintf("Product: %s, Qty: %d, Price: %d", sale.ProductID, sale.Quantity, sale.Price),
				DetectedAt:  time.Now(),
				FirstSeen:   ev.Timestamp,
				LastSeen:    ev.Timestamp,
				Count:       1,
			})
		}
	}

	return anomalies
}

func (e *Engine) detectStockDrift(events []Event) []Anomaly {
	var anomalies []Anomaly

	productStockIn := make(map[string]int64)
	productSales := make(map[string]int64)

	for _, ev := range events {
		switch ev.Type {
		case "stock":
			var stock core.StockItem
			if err := json.Unmarshal(ev.Data, &stock); err != nil {
				continue
			}
			productStockIn[stock.ProductID] += stock.Quantity
		case "sale":
			var sale core.Sale
			if err := json.Unmarshal(ev.Data, &sale); err != nil {
				continue
			}
			productSales[sale.ProductID] += sale.Quantity
		}
	}

	for productID, stockIn := range productStockIn {
		sold := productSales[productID]
		currentStock := e.inventory.Get(productID)

		expectedStock := stockIn - sold
		if expectedStock != currentStock {
			diff := expectedStock - currentStock
			severity := SeverityLow
			if diff > stockIn/10 {
				severity = SeverityMedium
			}
			if diff > stockIn/4 {
				severity = SeverityHigh
			}

			anomalies = append(anomalies, Anomaly{
				Type:        AnomalyStockDrift,
				Severity:    severity,
				ProductID:   productID,
				Description: fmt.Sprintf("Stock mismatch for %s: expected %d, got %d (unaccounted: %d)", productID, expectedStock, currentStock, diff),
				Details:     fmt.Sprintf("Total stock in: %d, Total sold: %d", stockIn, sold),
				DetectedAt:  time.Now(),
				Amount:      diff,
			})
		}
	}

	return anomalies
}

type saleRecord struct {
	Price     int64
	WorkerID  string
	Timestamp time.Time
}

func calculateRiskScore(anomalies []Anomaly) int {
	score := 0
	for _, a := range anomalies {
		switch a.Severity {
		case SeverityLow:
			score += 5
		case SeverityMedium:
			score += 15
		case SeverityHigh:
			score += 30
		case SeverityCritical:
			score += 50
		}
	}
	if score > 100 {
		score = 100
	}
	return score
}

func generateSummary(report GhostReport) string {
	if len(report.Anomalies) == 0 {
		return "No anomalies detected. All operations appear normal."
	}

	critical := 0
	high := 0
	for _, a := range report.Anomalies {
		switch a.Severity {
		case SeverityCritical:
			critical++
		case SeverityHigh:
			high++
		}
	}

	if critical > 0 {
		return fmt.Sprintf("CRITICAL: %d critical and %d high-severity anomalies detected. Immediate review recommended.", critical, high)
	}
	if high > 0 {
		return fmt.Sprintf("WARNING: %d high-severity anomalies detected. Review recommended.", high)
	}
	return fmt.Sprintf("%d low/medium anomalies detected. Monitor for patterns.", len(report.Anomalies))
}
