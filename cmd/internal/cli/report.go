package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	fromStr string
	toStr   string
	jsonOut bool
	csvOut  bool
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate operational reports",
}

var reportSalesCmd = &cobra.Command{
	Use:   "sales",
	Short: "Show sales summary",
	RunE: func(cmd *cobra.Command, args []string) error {

		from, to, err := parseDateRange()
		if err != nil {
			return err
		}

		summary := eng.SalesSummaryWithRange(from, to)

		// JSON output
		if jsonOut {
			return json.NewEncoder(os.Stdout).Encode(summary)
		}

		// CSV output
		if csvOut {
			writer := csv.NewWriter(os.Stdout)
			defer writer.Flush()

			writer.Write([]string{"total_sales", "cash_total", "mpesa_total"})
			writer.Write([]string{
				fmt.Sprintf("%d", summary.TotalSales),
				fmt.Sprintf("%d", summary.CashTotal),
				fmt.Sprintf("%d", summary.MpesaTotal),
			})
			return nil
		}

		// Default human output
		fmt.Println("📊 Sales Report")
		fmt.Printf("Total Sales: %d\n", summary.TotalSales)
		fmt.Printf("Cash: %d\n", summary.CashTotal)
		fmt.Printf("Mpesa: %d\n", summary.MpesaTotal)

		return nil
	},
}

func parseDateRange() (time.Time, time.Time, error) {
	var from, to time.Time
	var err error

	if fromStr != "" {
		from, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			return from, to, fmt.Errorf("invalid --from date format (use YYYY-MM-DD)")
		}
	}

	if toStr != "" {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			return from, to, fmt.Errorf("invalid --to date format (use YYYY-MM-DD)")
		}
	} else {
		to = time.Now()
	}

	return from, to, nil
}

var reportStockCmd = &cobra.Command{
	Use:   "stock",
	Short: "Show stock snapshot",
	RunE: func(cmd *cobra.Command, args []string) error {

		from, to, err := parseDateRange()
		if err != nil {
			return err
		}

		stock := eng.StockSnapshotWithRange(from, to)

		if jsonOut {
			return json.NewEncoder(os.Stdout).Encode(stock)
		}

		if csvOut {
			writer := csv.NewWriter(os.Stdout)
			defer writer.Flush()

			writer.Write([]string{"item", "quantity"})
			for item, qty := range stock {
				writer.Write([]string{item, fmt.Sprintf("%d", qty)})
			}
			return nil
		}

		fmt.Println("📦 Stock Report")
		for item, qty := range stock {
			fmt.Printf("%s: %d\n", item, qty)
		}

		return nil
	},
}

func init() {
	reportCmd.PersistentFlags().StringVar(&fromStr, "from", "", "Start date (YYYY-MM-DD)")
	reportCmd.PersistentFlags().StringVar(&toStr, "to", "", "End date (YYYY-MM-DD)")
	reportCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output JSON")
	reportCmd.PersistentFlags().BoolVar(&csvOut, "csv", false, "Output CSV")
	reportCmd.AddCommand(reportStockCmd)

	reportCmd.AddCommand(reportSalesCmd)
}
