package cli

import (
	"fmt"
	"mini-database/core"

	"strconv"

	"github.com/spf13/cobra"
)

var saleCmd = &cobra.Command{
	Use:   "sale",
	Short: "Register a sale",
	Run: func(cmd *cobra.Command, args []string) {
		product, _ := cmd.Flags().GetString("product")
		qtyStr, _ := cmd.Flags().GetString("qty")
		priceStr, _ := cmd.Flags().GetString("price")
		worker, _ := cmd.Flags().GetString("worker")
		paymentStr, _ := cmd.Flags().GetString("payment")

		qty, err := strconv.ParseInt(qtyStr, 10, 64)
		if err != nil {
			fmt.Println("Invalid quantity")
			return
		}
		price, err := strconv.ParseInt(priceStr, 10, 64)
		if err != nil {
			fmt.Println("Invalid price")
			return
		}

		var payment core.PaymentMethod
		switch paymentStr {
		case "cash":
			payment = core.PaymentCash
		case "mpesa":
			payment = core.PaymentMpesa
		default:
			fmt.Println("Invalid payment type, use 'cash' or 'mpesa'")
			return
		}

		s := core.Sale{
			ProductID: product,
			Quantity:  qty,
			Price:     price,
			WorkerID:  worker,
			Payment:   payment,
		}

		if err := eng.ApplySale(s); err != nil {
			fmt.Println("Error recording sale:", err)
			return
		}

		fmt.Printf("💰 Sale recorded: %d of %s at %d by %s via %s\n",
			qty, product, price, worker, paymentStr)
	},
}

func init() {
	saleCmd.Flags().String("product", "", "Product ID")
	saleCmd.Flags().String("qty", "", "Quantity sold")
	saleCmd.Flags().String("price", "", "Price per unit")
	saleCmd.Flags().String("worker", "", "Worker ID")
	saleCmd.Flags().String("payment", "", "Payment method: cash or mpesa")

	saleCmd.MarkFlagRequired("product")
	saleCmd.MarkFlagRequired("qty")
	saleCmd.MarkFlagRequired("price")
	saleCmd.MarkFlagRequired("worker")
	saleCmd.MarkFlagRequired("payment")
}
