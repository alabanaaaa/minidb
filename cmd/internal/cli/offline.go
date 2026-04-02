package cli

import (
	"fmt"
	"mini-database/internal/offline"

	"github.com/spf13/cobra"
)

var offlineQueue *offline.SyncQueue

var offlineCmd = &cobra.Command{
	Use:   "offline",
	Short: "Offline mode management",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: pos offline status|sync|queue")
	},
}

var offlineStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show offline/online status",
	Run: func(cmd *cobra.Command, args []string) {
		if offlineQueue == nil {
			fmt.Println("Offline mode not initialized")
			return
		}

		status := offlineQueue.GetConnectionStatus()
		pending, _ := offlineQueue.GetPendingCount()

		fmt.Printf("Status: %s\n", status)
		fmt.Printf("Pending sync items: %d\n", pending)

		if status == "offline" {
			fmt.Println("\n⚠️  Working offline - sales will be synced when connection is restored")
		}
	},
}

var offlineQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Show pending sync queue",
	Run: func(cmd *cobra.Command, args []string) {
		if offlineQueue == nil {
			fmt.Println("Offline mode not initialized")
			return
		}

		pending, _ := offlineQueue.GetPendingCount()
		fmt.Printf("Pending items: %d\n", pending)

		sales, _ := offlineQueue.GetLocalSales()
		if len(sales) > 0 {
			fmt.Println("\nPending sales:")
			for _, s := range sales {
				fmt.Printf("  - %s x%d by %s (%s)\n",
					s["product_id"], s["quantity"], s["worker_id"], s["payment"])
			}
		}
	},
}

var offlineInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize offline mode",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := offline.Config{
			DBPath:       "offline.db",
			ServerURL:    "http://localhost:8080",
			SyncInterval: 30,
			AutoSync:     true,
		}

		var err error
		offlineQueue, err = offline.NewSyncQueue(cfg)
		if err != nil {
			fmt.Printf("Failed to initialize offline mode: %v\n", err)
			return
		}

		fmt.Println("✓ Offline mode initialized")
		fmt.Println("  - Local SQLite storage ready")
		fmt.Println("  - Sync queue active")
		fmt.Println("  - Auto-sync enabled")
	},
}

func init() {
	offlineCmd.AddCommand(offlineStatusCmd)
	offlineCmd.AddCommand(offlineQueueCmd)
	offlineCmd.AddCommand(offlineInitCmd)
	rootCmd.AddCommand(offlineCmd)
}
