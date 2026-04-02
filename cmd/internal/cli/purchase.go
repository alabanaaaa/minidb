package cli

import (
	"fmt"
	"mini-database/internal/purchase"
	"time"

	"github.com/spf13/cobra"
)

var poManager *purchase.OrderManager

var purchaseCmd = &cobra.Command{
	Use:   "purchase",
	Short: "Purchase order management",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos purchase order|supplier|report")
	},
}

var purchaseOrderCmd = &cobra.Command{
	Use:   "order",
	Short: "Purchase order operations",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos purchase order create|list|receive|approve|status")
	},
}

var createPOCmd = &cobra.Command{
	Use:   "create [supplierID]",
	Short: "Create a new purchase order",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		supplierID := args[0]
		items := []purchase.POItem{
			{ProductID: "ITEM001", Quantity: 100, UnitCost: 50},
			{ProductID: "ITEM002", Quantity: 50, UnitCost: 75},
		}

		po, err := poManager.CreatePO(supplierID, items, "admin", "Bulk restock order")
		if err != nil {
			fmt.Printf("Error creating PO: %v\n", err)
			return
		}

		fmt.Printf("✓ Purchase Order created: %s\n", po.ID)
		fmt.Printf("  Supplier: %s\n", po.SupplierName)
		fmt.Printf("  Total: KES %d\n", po.TotalCost)
		fmt.Printf("  Status: %s\n", po.Status)
	},
}

var listPOCmd = &cobra.Command{
	Use:   "list",
	Short: "List all purchase orders",
	Run: func(cmd *cobra.Command, args []string) {
		pos := poManager.GetAllPOs()
		if len(pos) == 0 {
			fmt.Println("No purchase orders found")
			return
		}

		fmt.Println("\n=== Purchase Orders ===")
		for _, po := range pos {
			fmt.Printf("%s | %s | KES %d | %s\n",
				po.ID, po.SupplierName, po.TotalCost, po.Status)
		}
	},
}

var pendingPOCmd = &cobra.Command{
	Use:   "pending",
	Short: "Show pending orders",
	Run: func(cmd *cobra.Command, args []string) {
		pos := poManager.GetPendingOrders()
		if len(pos) == 0 {
			fmt.Println("No pending orders")
			return
		}

		fmt.Println("\n=== Pending Orders ===")
		for _, po := range pos {
			fmt.Printf("%s | %s | KES %d\n", po.ID, po.SupplierName, po.TotalCost)
			if po.ExpectedDate != nil {
				fmt.Printf("   Expected: %s\n", po.ExpectedDate.Format("2006-01-02"))
			}
		}
	},
}

var approvePOCmd = &cobra.Command{
	Use:   "approve [poID]",
	Short: "Approve a purchase order",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		poID := args[0]
		if err := poManager.SubmitPO(poID); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("✓ PO %s submitted\n", poID)

		if err := poManager.ApprovePO(poID); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("✓ PO %s approved\n", poID)
	},
}

var receivePOCmd = &cobra.Command{
	Use:   "receive [poID]",
	Short: "Mark order as fully received",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		poID := args[0]
		err := poManager.ReceiveFullPO(poID, time.Now())
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("✓ PO %s fully received\n", poID)
	},
}

var purchaseSupplierCmd = &cobra.Command{
	Use:   "supplier",
	Short: "Supplier management",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos purchase supplier add|list")
	},
}

var addSupplierCmd = &cobra.Command{
	Use:   "add [name]",
	Short: "Add a new supplier",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		supplier := purchase.Supplier{
			ID:           "SUP" + fmt.Sprintf("%d", time.Now().Unix()),
			Name:         name,
			Contact:      "Contact Person",
			Phone:        "+254700000000",
			Email:        "supplier@example.com",
			LeadTimeDays: 7,
		}

		if err := poManager.AddSupplier(supplier); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		fmt.Printf("✓ Supplier added: %s (%s)\n", supplier.Name, supplier.ID)
	},
}

var listSupplierCmd = &cobra.Command{
	Use:   "list",
	Short: "List all suppliers",
	Run: func(cmd *cobra.Command, args []string) {
		suppliers := poManager.GetAllSuppliers()
		if len(suppliers) == 0 {
			fmt.Println("No suppliers found")
			return
		}

		fmt.Println("\n=== Suppliers ===")
		for _, s := range suppliers {
			fmt.Printf("%s | %s | %s | Lead: %d days\n",
				s.ID, s.Name, s.Phone, s.LeadTimeDays)
		}
	},
}

var purchaseReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate purchase report",
	Run: func(cmd *cobra.Command, args []string) {
		report := poManager.GenerateReport()

		fmt.Println("\n=== Purchase Order Report ===")
		fmt.Printf("Total Orders: %d\n", report.TotalOrders)
		fmt.Printf("Total Value: KES %d\n", report.TotalValue)
		fmt.Printf("Pending Value: KES %d\n", report.PendingValue)
		fmt.Printf("Received Value: KES %d\n", report.ReceivedValue)
		fmt.Printf("Avg Order Value: KES %d\n", report.AvgOrderValue)

		fmt.Println("\nBy Status:")
		for status, count := range report.ByStatus {
			fmt.Printf("  %s: %d\n", status, count)
		}

		if len(report.TopSuppliers) > 0 {
			fmt.Println("\nTop Suppliers:")
			for _, s := range report.TopSuppliers {
				fmt.Printf("  %s: KES %d (%d orders)\n",
					s.SupplierName, s.TotalSpent, s.OrderCount)
			}
		}
	},
}

func init() {
	poManager = purchase.NewOrderManager()

	po := []purchase.POItem{
		{ProductID: "PROD001", ProductName: "Sample Item", Quantity: 100, UnitCost: 100, TotalCost: 10000},
	}
	poManager.CreatePO("SUP001", po, "system", "Demo PO")

	poManager.AddSupplier(purchase.Supplier{
		ID: "SUP001", Name: "Kenya Wholesalers Ltd", Phone: "+254700111222",
		Email: "orders@kenyawholesalers.co.ke", LeadTimeDays: 5,
	})
	poManager.AddSupplier(purchase.Supplier{
		ID: "SUP002", Name: "Nairobi Distributors", Phone: "+254700333444",
		Email: "sales@nairobidist.co.ke", LeadTimeDays: 3,
	})

	purchaseOrderCmd.AddCommand(createPOCmd)
	purchaseOrderCmd.AddCommand(listPOCmd)
	purchaseOrderCmd.AddCommand(pendingPOCmd)
	purchaseOrderCmd.AddCommand(approvePOCmd)
	purchaseOrderCmd.AddCommand(receivePOCmd)

	purchaseSupplierCmd.AddCommand(addSupplierCmd)
	purchaseSupplierCmd.AddCommand(listSupplierCmd)

	purchaseCmd.AddCommand(purchaseOrderCmd)
	purchaseCmd.AddCommand(purchaseSupplierCmd)
	purchaseCmd.AddCommand(purchaseReportCmd)

	rootCmd.AddCommand(purchaseCmd)
}
