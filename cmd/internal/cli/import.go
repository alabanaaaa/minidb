package cli

import (
	"fmt"
	"mini-database/internal/importcsv"
	"os"

	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import data from CSV",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos import inventory|workers|sales [file.csv]")
	},
}

var importInventoryCmd = &cobra.Command{
	Use:   "inventory [file.csv]",
	Short: "Import inventory from CSV",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("File not found: %s\n", path)
			return
		}

		count, err := importcsv.ImportInventoryCSV(path, eng)
		if err != nil {
			fmt.Printf("Import failed: %v\n", err)
			return
		}

		fmt.Printf("✓ Imported %d inventory items from %s\n", count, path)
	},
}

var importSalesCmd = &cobra.Command{
	Use:   "sales [file.csv]",
	Short: "Import sales from CSV",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("File not found: %s\n", path)
			return
		}

		count, err := importcsv.ImportSalesCSV(path, eng)
		if err != nil {
			fmt.Printf("Import failed: %v\n", err)
			return
		}

		fmt.Printf("✓ Imported %d sales from %s\n", count, path)
	},
}

var importWorkersCmd = &cobra.Command{
	Use:   "workers [file.csv]",
	Short: "Import workers from CSV",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("File not found: %s\n", path)
			return
		}

		workers, err := importcsv.ImportWorkersCSV(path)
		if err != nil {
			fmt.Printf("Import failed: %v\n", err)
			return
		}

		fmt.Printf("✓ Imported %d workers from %s\n", len(workers), path)
		for _, w := range workers {
			fmt.Printf("  - %s: %s (%s)\n", w.WorkerID, w.Name, w.Role)
		}
	},
}

var exportInventoryCmd = &cobra.Command{
	Use:   "export [file.csv]",
	Short: "Export inventory to CSV",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		inventory := eng.InventorySnapshot()

		if err := importcsv.ExportInventoryCSV(path, inventory); err != nil {
			fmt.Printf("Export failed: %v\n", err)
			return
		}

		fmt.Printf("✓ Exported %d items to %s\n", len(inventory), path)
	},
}

func init() {
	importCmd.AddCommand(importInventoryCmd)
	importCmd.AddCommand(importSalesCmd)
	importCmd.AddCommand(importWorkersCmd)
	importCmd.AddCommand(exportInventoryCmd)
	rootCmd.AddCommand(importCmd)
}
