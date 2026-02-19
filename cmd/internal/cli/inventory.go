package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Inspect current inventory state",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Inventory inspection not yet wired to engine")
	},
}

func init() {
	rootCmd.AddCommand(inventoryCmd)
}
