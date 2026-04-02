package engine

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"mini-database/core"
)

type ForecastConfig struct {
	HistoricalDays int
	Seasonality    bool
	TrendWeight    float64
}

type InventoryForecast struct {
	ProductID        string  `json:"product_id"`
	CurrentStock     int64   `json:"current_stock"`
	DailyAvgSales    float64 `json:"daily_avg_sales"`
	DaysUntilStock   int     `json:"days_until_stock"`
	ReorderPoint     int64   `json:"reorder_point"`
	RecommendedOrder int64   `json:"recommended_order"`
	ForecastDemand   []int64 `json:"forecast_demand"`
	Confidence       float64 `json:"confidence"`
}

type RevenueForecast struct {
	Period        string           `json:"period"`
	HistoricalAvg float64          `json:"historical_avg"`
	Projected     float64          `json:"projected"`
	GrowthRate    float64          `json:"growth_rate"`
	Last7Days     int64            `json:"last_7_days"`
	Last30Days    int64            `json:"last_30_days"`
	Next7Days     float64          `json:"next_7_days"`
	Next30Days    float64          `json:"next_30_days"`
	Confidence    float64          `json:"confidence"`
	ByWorker      map[string]int64 `json:"by_worker"`
}

func (e *Engine) GetInventoryForecast(productID string, cfg ForecastConfig) *InventoryForecast {
	if cfg.HistoricalDays == 0 {
		cfg.HistoricalDays = 30
	}
	if cfg.TrendWeight == 0 {
		cfg.TrendWeight = 0.3
	}

	stock := e.inventory.Get(productID)
	sales := e.getProductSales(productID)

	if len(sales) == 0 {
		return &InventoryForecast{
			ProductID:    productID,
			CurrentStock: stock,
			Confidence:   0,
		}
	}

	dailySales := e.calculateDailyAverage(sales, cfg.HistoricalDays)
	trend := e.calculateTrend(sales)
	seasonality := e.calculateSeasonality(sales)

	predictedDaily := dailySales * (1 + cfg.TrendWeight*trend) * seasonality

	var daysUntilStock int
	if predictedDaily > 0 {
		daysUntilStock = int(float64(stock) / predictedDaily)
	}

	reorderPoint := int64(dailySales * 7)
	recommendedOrder := int64(predictedDaily * 14)

	confidence := e.calculateConfidence(len(sales), trend)

	return &InventoryForecast{
		ProductID:        productID,
		CurrentStock:     stock,
		DailyAvgSales:    dailySales,
		DaysUntilStock:   daysUntilStock,
		ReorderPoint:     reorderPoint,
		RecommendedOrder: recommendedOrder,
		Confidence:       confidence,
	}
}

func (e *Engine) GetAllInventoryForecasts(cfg ForecastConfig) []*InventoryForecast {
	stock := e.InventorySnapshot()
	var forecasts []*InventoryForecast

	for productID := range stock {
		fc := e.GetInventoryForecast(productID, cfg)
		forecasts = append(forecasts, fc)
	}

	sort.Slice(forecasts, func(i, j int) bool {
		return forecasts[i].DaysUntilStock < forecasts[j].DaysUntilStock
	})

	return forecasts
}

func (e *Engine) GetRevenueForecast(cfg ForecastConfig) *RevenueForecast {
	if cfg.HistoricalDays == 0 {
		cfg.HistoricalDays = 30
	}

	now := time.Now()
	last7Days := now.AddDate(0, 0, -7)
	last30Days := now.AddDate(0, 0, -30)

	r7 := e.SalesSummaryWithRange(last7Days, now)
	r30 := e.SalesSummaryWithRange(last30Days, now)

	avgDaily7 := float64(r7.TotalSales) / 7
	avgDaily30 := float64(r30.TotalSales) / 30

	growthRate := 0.0
	if avgDaily30 > 0 {
		growthRate = (avgDaily7 - avgDaily30) / avgDaily30
	}

	next7Days := avgDaily7 * 7 * (1 + growthRate)
	next30Days := avgDaily30 * 30 * (1 + growthRate)

	confidence := 0.85
	if r7.TotalSales == 0 {
		confidence = 0.5
	}

	byWorker := e.getWorkerRevenueMap()

	return &RevenueForecast{
		Period:        "weekly",
		HistoricalAvg: avgDaily30,
		Projected:     next7Days,
		GrowthRate:    growthRate,
		Last7Days:     r7.TotalSales,
		Last30Days:    r30.TotalSales,
		Next7Days:     next7Days,
		Next30Days:    next30Days,
		Confidence:    confidence,
		ByWorker:      byWorker,
	}
}

func (e *Engine) getProductSales(productID string) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var sales []Event
	for _, evt := range e.events {
		if evt.Type != "sale" {
			continue
		}
		var sale core.Sale
		if err := json.Unmarshal(evt.Data, &sale); err != nil {
			continue
		}
		if sale.ProductID == productID {
			sales = append(sales, evt)
		}
	}
	return sales
}

func (e *Engine) calculateDailyAverage(sales []Event, days int) float64 {
	if len(sales) == 0 {
		return 0
	}

	var total int64
	for _, s := range sales {
		var sale core.Sale
		json.Unmarshal(s.Data, &sale)
		total += sale.Quantity * sale.Price
	}

	return float64(total) / float64(days)
}

func (e *Engine) calculateTrend(sales []Event) float64 {
	if len(sales) < 7 {
		return 0
	}

	mid := len(sales) / 2
	var firstHalf, secondHalf int64

	for _, s := range sales[:mid] {
		var sale core.Sale
		json.Unmarshal(s.Data, &sale)
		firstHalf += sale.Quantity * sale.Price
	}
	for _, s := range sales[mid:] {
		var sale core.Sale
		json.Unmarshal(s.Data, &sale)
		secondHalf += sale.Quantity * sale.Price
	}

	if firstHalf == 0 {
		return 0
	}

	return float64(secondHalf-firstHalf) / float64(firstHalf)
}

func (e *Engine) calculateSeasonality(sales []Event) float64 {
	if len(sales) < 14 {
		return 1.0
	}

	now := time.Now()
	weekday := now.Weekday()

	var sameDayTotal, otherDayTotal int64
	var sameDayCount, otherDayCount int

	for _, s := range sales {
		if s.Timestamp.Weekday() == weekday {
			var sale core.Sale
			json.Unmarshal(s.Data, &sale)
			sameDayTotal += sale.Quantity * sale.Price
			sameDayCount++
		} else {
			var sale core.Sale
			json.Unmarshal(s.Data, &sale)
			otherDayTotal += sale.Quantity * sale.Price
			otherDayCount++
		}
	}

	if otherDayCount == 0 {
		return 1.0
	}

	avgSame := float64(sameDayTotal) / float64(sameDayCount)
	avgOther := float64(otherDayTotal) / float64(otherDayCount)

	if avgOther == 0 {
		return 1.0
	}

	return math.Min(1.5, math.Max(0.5, avgSame/avgOther))
}

func (e *Engine) calculateConfidence(salesCount int, trend float64) float64 {
	baseConfidence := math.Min(1.0, float64(salesCount)/30.0)

	trendPenalty := math.Abs(trend) * 0.1

	confidence := baseConfidence - trendPenalty
	return math.Max(0.3, math.Min(0.95, confidence))
}

func (e *Engine) getWorkerRevenueMap() map[string]int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	last7Days := now.AddDate(0, 0, -7)

	workerRevenue := make(map[string]int64)

	for _, evt := range e.events {
		if evt.Type != "sale" {
			continue
		}
		if evt.Timestamp.Before(last7Days) {
			continue
		}

		var sale core.Sale
		if err := json.Unmarshal(evt.Data, &sale); err != nil {
			continue
		}

		amount := sale.Price * sale.Quantity
		workerRevenue[sale.WorkerID] += amount
	}

	return workerRevenue
}

type LowStockAlert struct {
	ProductID      string `json:"product_id"`
	CurrentStock   int64  `json:"current_stock"`
	ReorderPoint   int64  `json:"reorder_point"`
	Urgency        string `json:"urgency"`
	DaysUntilStock int    `json:"days_until_stock"`
}

func (e *Engine) GetLowStockAlerts() []LowStockAlert {
	forecasts := e.GetAllInventoryForecasts(ForecastConfig{})

	var alerts []LowStockAlert
	for _, fc := range forecasts {
		if fc.DaysUntilStock <= 7 || fc.CurrentStock <= fc.ReorderPoint {
			urgency := "low"
			if fc.DaysUntilStock <= 2 {
				urgency = "critical"
			} else if fc.DaysUntilStock <= 5 {
				urgency = "high"
			}

			alerts = append(alerts, LowStockAlert{
				ProductID:      fc.ProductID,
				CurrentStock:   fc.CurrentStock,
				ReorderPoint:   fc.ReorderPoint,
				Urgency:        urgency,
				DaysUntilStock: fc.DaysUntilStock,
			})
		}
	}

	sort.Slice(alerts, func(i, j int) bool {
		urgencyOrder := map[string]int{"critical": 0, "high": 1, "low": 2}
		return urgencyOrder[alerts[i].Urgency] < urgencyOrder[alerts[j].Urgency]
	})

	return alerts
}

type DemandTrend struct {
	ProductID  string  `json:"product_id"`
	Trend      string  `json:"trend"`
	ChangePct  float64 `json:"change_pct"`
	Prediction string  `json:"prediction"`
}

func (e *Engine) GetDemandTrends() []DemandTrend {
	stock := e.InventorySnapshot()
	var trends []DemandTrend

	for productID := range stock {
		sales := e.getProductSales(productID)
		if len(sales) < 7 {
			continue
		}

		trend := e.calculateTrend(sales)

		var trendStr string
		var prediction string
		var changePct float64

		if trend > 0.1 {
			trendStr = "rising"
			prediction = "Increase stock"
			changePct = trend * 100
		} else if trend < -0.1 {
			trendStr = "falling"
			prediction = "Consider markdown"
			changePct = trend * 100
		} else {
			trendStr = "stable"
			prediction = "Maintain current levels"
			changePct = 0
		}

		trends = append(trends, DemandTrend{
			ProductID:  productID,
			Trend:      trendStr,
			ChangePct:  changePct,
			Prediction: prediction,
		})
	}

	sort.Slice(trends, func(i, j int) bool {
		return math.Abs(trends[i].ChangePct) > math.Abs(trends[j].ChangePct)
	})

	return trends[:10]
}
