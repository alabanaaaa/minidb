package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var ghostCmd = &cobra.Command{
	Use:   "ghost",
	Short: "Run Ghost Mode anomaly detection on the event ledger",
}

var ghostRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run anomaly detection",
	Run: func(cmd *cobra.Command, args []string) {
		fromStr, _ := cmd.Flags().GetString("from")
		toStr, _ := cmd.Flags().GetString("to")

		var from, to time.Time
		var err error

		if fromStr != "" {
			from, err = time.Parse("2006-01-02", fromStr)
			if err != nil {
				fmt.Println("Invalid --from date format (use YYYY-MM-DD)")
				return
			}
		}

		if toStr != "" {
			to, err = time.Parse("2006-01-02", toStr)
			if err != nil {
				fmt.Println("Invalid --to date format (use YYYY-MM-DD)")
				return
			}
		} else {
			to = time.Now()
		}

		report := eng.RunGhostMode(from, to)

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			data, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(data))
			return
		}

		fmt.Println("👻 Ghost Mode Report")
		fmt.Println("====================")
		fmt.Printf("Period: %s to %s\n", report.PeriodStart.Format("2006-01-02"), report.PeriodEnd.Format("2006-01-02"))
		fmt.Printf("Events analyzed: %d\n", report.TotalEvents)
		fmt.Printf("Anomalies found: %d\n", len(report.Anomalies))
		fmt.Printf("Risk Score: %d/100\n", report.RiskScore)
		fmt.Println()
		fmt.Printf("Summary: %s\n", report.Summary)
		fmt.Println()

		if len(report.Anomalies) > 0 {
			fmt.Println("--- Anomalies ---")
			for i, a := range report.Anomalies {
				severityIcon := "⚠️"
				switch a.Severity {
				case "critical":
					severityIcon = "🔴"
				case "high":
					severityIcon = "🟠"
				case "medium":
					severityIcon = "🟡"
				case "low":
					severityIcon = "🟢"
				}
				fmt.Printf("\n%d. %s [%s] %s\n", i+1, severityIcon, a.Severity, a.Type)
				fmt.Printf("   %s\n", a.Description)
				if a.Details != "" {
					fmt.Printf("   %s\n", a.Details)
				}
				if a.WorkerID != "" {
					fmt.Printf("   Worker: %s\n", a.WorkerID)
				}
				if a.ProductID != "" {
					fmt.Printf("   Product: %s\n", a.ProductID)
				}
			}
		}
	},
}

func init() {
	ghostRunCmd.Flags().String("from", "", "Start date (YYYY-MM-DD)")
	ghostRunCmd.Flags().String("to", "", "End date (YYYY-MM-DD)")
	ghostRunCmd.Flags().Bool("json", false, "Output as JSON")
	ghostCmd.AddCommand(ghostRunCmd)
}
