package cli

import (
	"fmt"

	"strconv"

	"github.com/spf13/cobra"
)

var reconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Reconcile a worker's sales",
	Run: func(cmd *cobra.Command, args []string) {
		worker, _ := cmd.Flags().GetString("worker")
		cashStr, _ := cmd.Flags().GetString("cash")
		mpesaStr, _ := cmd.Flags().GetString("mpesa")

		cash, err := strconv.ParseInt(cashStr, 10, 64)
		if err != nil {
			fmt.Println("Invalid cash amount")
			return
		}

		mpesa, err := strconv.ParseInt(mpesaStr, 10, 64)
		if err != nil {
			fmt.Println("Invalid mpesa amount")
			return
		}

		rec, err := eng.Reconcile(worker, cash, mpesa)
		if err != nil {
			fmt.Println("Reconciliation failed:", err)
			return
		}

		fmt.Printf("✅ Reconciliation complete for %s\n", worker)
		fmt.Printf("Expected Cash: %d, Declared Cash: %d, Variance: %d\n",
			rec.ExpectedCash, rec.DeclaredCash, rec.CashVariance)
		fmt.Printf("Expected Mpesa: %d, Declared Mpesa: %d, Variance: %d\n",
			rec.ExpectedMpesa, rec.DeclaredMpesa, rec.MpesaVariance)
	},
}

func init() {
	reconcileCmd.Flags().String("worker", "", "Worker ID")
	reconcileCmd.Flags().String("cash", "", "Declared cash")
	reconcileCmd.Flags().String("mpesa", "", "Declared mpesa")

	reconcileCmd.MarkFlagRequired("worker")
	reconcileCmd.MarkFlagRequired("cash")
	reconcileCmd.MarkFlagRequired("mpesa")
}
