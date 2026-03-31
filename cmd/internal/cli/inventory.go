package cli

import (
	"fmt"
	"mini-database/core"
	"mini-database/engine"
	"strconv"

	"github.com/spf13/cobra"
)

var eng *engine.Engine

// Initialize engine with DB, panic on error
func init() {
	var err error
	eng, err = engine.NewEngineWithDB("pos.db")
	if err != nil {
		panic("failed to open engine DB: " + err.Error())
	}
}

// Inventory root command
var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Manage stock and sales",
}

// Add stock command
var addStockCmd = &cobra.Command{
	Use:   "add [productID] [quantity] [cost]",
	Short: "Add stock for a product",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		qty, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Println("Invalid quantity:", err)
			return
		}
		cost, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			fmt.Println("Invalid cost:", err)
			return
		}

		stock := core.StockItem{
			ProductID: args[0],
			Quantity:  qty,
			Cost:      cost,
		}

		if err := eng.ApplyStock(stock); err != nil {
			fmt.Println("Error adding stock:", err)
			return
		}

		fmt.Printf("Added %d units of %s at cost %d\n", qty, args[0], cost)
	},
}

// Record sale command - REMOVED: Use ./pos sale --product ... instead
// The sale command with flags is now the canonical way to record sales

// Check stock command
var checkStockCmd = &cobra.Command{
	Use:   "check [productID]",
	Short: "Check current stock of a product",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		qty := eng.GetStock(args[0])
		fmt.Printf("Current stock of %s: %d\n", args[0], qty)
	},
}

func init() {
	inventoryCmd.AddCommand(addStockCmd)
	inventoryCmd.AddCommand(checkStockCmd)
}
