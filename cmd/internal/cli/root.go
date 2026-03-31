package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pos",
	Short: "Retail financial integrity engine",
	Long:  "Operator console for Pos",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(inventoryCmd)
	rootCmd.AddCommand(saleCmd)
	rootCmd.AddCommand(reconcileCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(simulateCmd)
	rootCmd.AddCommand(ledgerCmd)
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(ghostCmd)

}
