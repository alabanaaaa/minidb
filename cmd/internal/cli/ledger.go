package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	page  int
	limit int
)

var ledgerCmd = &cobra.Command{
	Use:   "ledger",
	Short: "Inspect event ledger",
}

var ledgerShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show all events",
	Run: func(cmd *cobra.Command, args []string) {
		events := eng.PaginatedEvents(page*limit, limit)
		for i, evt := range events {
			fmt.Printf("%d | %s | %s\n", page*limit+i, evt.Timestamp.Format(time.RFC3339), evt.Type)
		}
	},
}

var ledgerFilterCmd = &cobra.Command{
	Use:   "filter [type]",
	Short: "Filter events by type (sale, stock, reconcile)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		filterType := args[0]
		events := eng.EventsByType(filterType)

		for i, evt := range events {
			fmt.Printf("%d | %s | %s\n", i, evt.Timestamp.Format(time.RFC3339), evt.Type)
		}
	},
}

func init() {
	ledgerCmd.AddCommand(ledgerShowCmd)
	ledgerCmd.AddCommand(ledgerFilterCmd)

	ledgerShowCmd.Flags().IntVar(&page, "page", 0, "Page number (starting from 0)")
	ledgerShowCmd.Flags().IntVar(&limit, "limit", 10, "Number of events per page")
}
