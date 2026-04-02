package cli

import (
	"fmt"
	"mini-database/internal/receipt"
	"os"

	"github.com/spf13/cobra"
)

var receiptCmd = &cobra.Command{
	Use:   "receipt",
	Short: "Generate receipt PDF",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos receipt generate [event_index] [output.pdf]")
	},
}

var generateReceiptCmd = &cobra.Command{
	Use:   "generate [event_index] [output.pdf]",
	Short: "Generate PDF receipt for a sale event",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		var eventIndex int
		fmt.Sscanf(args[0], "%d", &eventIndex)
		outputPath := args[1]

		r := receipt.Receipt{
			ShopName:    "Mini-Database POS",
			ShopAddress: "Nairobi, Kenya",
			ReceiptNo:   fmt.Sprintf("RCP-%d", eventIndex),
			Items: []receipt.ReceiptItem{
				{Name: "Sample Item", Quantity: 1, Price: 100, Total: 100},
			},
			Subtotal: 100,
			Tax:      0,
			Total:    100,
			Payment:  "Cash",
			Worker:   "Worker1",
			Currency: "KES",
		}

		pdfBytes, err := receipt.GeneratePDF(r)
		if err != nil {
			fmt.Printf("Error generating receipt: %v\n", err)
			return
		}

		if err := os.WriteFile(outputPath, pdfBytes, 0644); err != nil {
			fmt.Printf("Error writing PDF: %v\n", err)
			return
		}

		fmt.Printf("✓ Receipt generated: %s\n", outputPath)
	},
}

var generateReceiptFullCmd = &cobra.Command{
	Use:   "full [event_index] [output.pdf]",
	Short: "Generate full receipt from engine event",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		var eventIndex int
		fmt.Sscanf(args[0], "%d", &eventIndex)
		outputPath := args[1]

		pdfBytes, err := eng.GenerateReceipt(eventIndex)
		if err != nil {
			fmt.Printf("Error generating receipt: %v\n", err)
			return
		}

		if err := os.WriteFile(outputPath, pdfBytes, 0644); err != nil {
			fmt.Printf("Error writing PDF: %v\n", err)
			return
		}

		fmt.Printf("✓ Receipt generated: %s\n", outputPath)
	},
}

func init() {
	receiptCmd.AddCommand(generateReceiptCmd)
	receiptCmd.AddCommand(generateReceiptFullCmd)
	rootCmd.AddCommand(receiptCmd)
}
