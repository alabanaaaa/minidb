package cli

import (
	"encoding/json"
	"fmt"
	"mini-database/engine"

	"github.com/spf13/cobra"
)

var forecastCmd = &cobra.Command{
	Use:   "forecast",
	Short: "Inventory and revenue forecasting",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos forecast inventory|revenue|alerts|trends")
	},
}

var forecastInventoryCmd = &cobra.Command{
	Use:   "inventory [productID]",
	Short: "Forecast inventory demand",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := engine.ForecastConfig{
			HistoricalDays: 30,
			TrendWeight:    0.3,
		}

		if len(args) == 1 {
			fc := eng.GetInventoryForecast(args[0], cfg)
			printInventoryForecast(*fc)
		} else {
			forecasts := eng.GetAllInventoryForecasts(cfg)
			fmt.Println("\n=== Inventory Forecasts ===")
			for _, fc := range forecasts {
				printInventoryForecast(*fc)
			}
		}
	},
}

var forecastRevenueCmd = &cobra.Command{
	Use:   "revenue",
	Short: "Forecast revenue trends",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := engine.ForecastConfig{
			HistoricalDays: 30,
		}
		fc := eng.GetRevenueForecast(cfg)
		printRevenueForecast(*fc)
	},
}

var forecastAlertsCmd = &cobra.Command{
	Use:   "alerts",
	Short: "Show low stock alerts with reorder suggestions",
	Run: func(cmd *cobra.Command, args []string) {
		alerts := eng.GetLowStockAlerts()
		if len(alerts) == 0 {
			fmt.Println("✓ No low stock alerts")
			return
		}

		fmt.Println("\n=== Low Stock Alerts ===")
		for _, alert := range alerts {
			urgencyIcon := "⚠️"
			if alert.Urgency == "critical" {
				urgencyIcon = "🚨"
			}
			fmt.Printf("%s [%s] %s\n", urgencyIcon, alert.Urgency, alert.ProductID)
			fmt.Printf("   Stock: %d | Reorder at: %d | Days left: %d\n",
				alert.CurrentStock, alert.ReorderPoint, alert.DaysUntilStock)
		}
	},
}

var forecastTrendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Show product demand trends",
	Run: func(cmd *cobra.Command, args []string) {
		trends := eng.GetDemandTrends()
		if len(trends) == 0 {
			fmt.Println("Insufficient sales data for trend analysis")
			return
		}

		fmt.Println("\n=== Demand Trends (Top 10) ===")
		for _, t := range trends {
			icon := "➡️"
			if t.Trend == "rising" {
				icon = "📈"
			} else if t.Trend == "falling" {
				icon = "📉"
			}
			fmt.Printf("%s %s: %s (%+.1f%%)\n", icon, t.ProductID, t.Trend, t.ChangePct)
			fmt.Printf("   → %s\n", t.Prediction)
		}
	},
}

var forecastJSONCmd = &cobra.Command{
	Use:   "json",
	Short: "Export all forecast data as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := engine.ForecastConfig{HistoricalDays: 30}

		data := map[string]interface{}{
			"inventory_forecasts": eng.GetAllInventoryForecasts(cfg),
			"revenue_forecast":    eng.GetRevenueForecast(cfg),
			"low_stock_alerts":    eng.GetLowStockAlerts(),
			"demand_trends":       eng.GetDemandTrends(),
		}

		jsonBytes, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(jsonBytes))
	},
}

func printInventoryForecast(fc engine.InventoryForecast) {
	fmt.Printf("\n📦 %s\n", fc.ProductID)
	fmt.Printf("   Current Stock: %d\n", fc.CurrentStock)
	fmt.Printf("   Daily Avg Sales: %.1f\n", fc.DailyAvgSales)
	fmt.Printf("   Days Until Stockout: %d\n", fc.DaysUntilStock)
	fmt.Printf("   Reorder Point: %d\n", fc.ReorderPoint)
	fmt.Printf("   Recommended Order: %d\n", fc.RecommendedOrder)
	fmt.Printf("   Confidence: %.0f%%\n", fc.Confidence*100)
}

func printRevenueForecast(fc engine.RevenueForecast) {
	fmt.Println("\n💰 Revenue Forecast")
	fmt.Printf("   Last 7 Days: KES %d\n", fc.Last7Days)
	fmt.Printf("   Last 30 Days: KES %d\n", fc.Last30Days)
	fmt.Printf("   Next 7 Days (proj): KES %.0f\n", fc.Next7Days)
	fmt.Printf("   Next 30 Days (proj): KES %.0f\n", fc.Next30Days)
	if fc.GrowthRate > 0 {
		fmt.Printf("   Growth Rate: +%.1f%% 📈\n", fc.GrowthRate*100)
	} else if fc.GrowthRate < 0 {
		fmt.Printf("   Growth Rate: %.1f%% 📉\n", fc.GrowthRate*100)
	}
	fmt.Printf("   Confidence: %.0f%%\n", fc.Confidence*100)

	if len(fc.ByWorker) > 0 {
		fmt.Println("\n   Top Workers (Last 7 Days):")
		for workerID, revenue := range fc.ByWorker {
			fmt.Printf("   - %s: KES %d\n", workerID, revenue)
		}
	}
}

func init() {
	forecastCmd.AddCommand(forecastInventoryCmd)
	forecastCmd.AddCommand(forecastRevenueCmd)
	forecastCmd.AddCommand(forecastAlertsCmd)
	forecastCmd.AddCommand(forecastTrendsCmd)
	forecastCmd.AddCommand(forecastJSONCmd)
	rootCmd.AddCommand(forecastCmd)
}
