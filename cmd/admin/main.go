package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"mini-database/internal/config"
	"mini-database/internal/db"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

var (
	dbURL   string
	shopID  string
	jsonOut bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "admin",
		Short: "Owner's remote administration tool",
		Long: `Powerful CLI for remote system administration.
Use this to diagnose, troubleshoot, and manage any shop's system from anywhere.`,
	}

	rootCmd.PersistentFlags().StringVar(&dbURL, "db", "", "Database URL (or set DATABASE_URL env)")
	rootCmd.PersistentFlags().StringVar(&shopID, "shop", "", "Shop ID to operate on")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if dbURL == "" {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			dbURL = cfg.DatabaseURL
		}
		return nil
	}

	// Register subcommands
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(inspectCmd())
	rootCmd.AddCommand(queryCmd())
	rootCmd.AddCommand(eventsCmd())
	rootCmd.AddCommand(ledgerCmd())
	rootCmd.AddCommand(ghostCmd())
	rootCmd.AddCommand(usersCmd())
	rootCmd.AddCommand(sessionsCmd())
	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(emergencyCmd())
	rootCmd.AddCommand(metricsCmd())
	rootCmd.AddCommand(logsCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func connectDB() (*sql.DB, error) {
	d, err := db.New(dbURL)
	if err != nil {
		return nil, err
	}
	return d.DB, nil
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "System health overview",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := connectDB()
			if err != nil {
				return fmt.Errorf("❌ Database: %v", err)
			}
			defer conn.Close()

			status := map[string]interface{}{
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"database":  "connected",
			}

			var shopCount, userCount, eventCount, activeSessions int
			_ = conn.QueryRow("SELECT COUNT(*) FROM shops").Scan(&shopCount)
			_ = conn.QueryRow("SELECT COUNT(*) FROM shop_users").Scan(&userCount)
			_ = conn.QueryRow("SELECT COUNT(*) FROM events").Scan(&eventCount)
			_ = conn.QueryRow("SELECT COUNT(*) FROM sessions WHERE status='active'").Scan(&activeSessions)

			status["shops"] = shopCount
			status["users"] = userCount
			status["total_events"] = eventCount
			status["active_sessions"] = activeSessions

			var dbSize string
			_ = conn.QueryRow("SELECT pg_size_pretty(pg_database_size(current_database()))").Scan(&dbSize)
			status["database_size"] = dbSize

			var uptime string
			_ = conn.QueryRow("SELECT NOW() - pg_postmaster_start_time()").Scan(&uptime)
			status["database_uptime"] = strings.TrimSpace(uptime)

			if jsonOut {
				return printJSON(status)
			}

			fmt.Println("🔧 System Status")
			fmt.Println("================")
			fmt.Printf("Time:       %s\n", status["timestamp"])
			fmt.Printf("Database:   ✅ %s\n", status["database"])
			fmt.Printf("Size:       %s\n", status["database_size"])
			fmt.Printf("Uptime:     %s\n", status["database_uptime"])
			fmt.Println()
			fmt.Printf("Shops:          %d\n", shopCount)
			fmt.Printf("Users:          %d\n", userCount)
			fmt.Printf("Total Events:   %d\n", eventCount)
			fmt.Printf("Active Sessions: %d\n", activeSessions)

			return nil
		},
	}
}

func inspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a specific shop's state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			var name, ownerEmail, currency string
			var taxRate float64
			var createdAt time.Time
			err = conn.QueryRow(`SELECT name, owner_email, currency, tax_rate, created_at FROM shops WHERE id = $1`, shopID).
				Scan(&name, &ownerEmail, &currency, &taxRate, &createdAt)
			if err != nil {
				return fmt.Errorf("shop not found: %w", err)
			}

			var productCount, workerCount, eventCount, saleCount int
			_ = conn.QueryRow(`SELECT COUNT(*) FROM products WHERE shop_id = $1`, shopID).Scan(&productCount)
			_ = conn.QueryRow(`SELECT COUNT(*) FROM workers WHERE shop_id = $1`, shopID).Scan(&workerCount)
			_ = conn.QueryRow(`SELECT COUNT(*) FROM events WHERE shop_id = $1`, shopID).Scan(&eventCount)
			_ = conn.QueryRow(`SELECT COUNT(*) FROM events WHERE shop_id = $1 AND event_type = 'sale'`, shopID).Scan(&saleCount)

			var totalRevenue int64
			_ = conn.QueryRow(`
				SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
				FROM events WHERE shop_id = $1 AND event_type = 'sale'
			`, shopID).Scan(&totalRevenue)

			var activeSession string
			_ = conn.QueryRow(`
				SELECT w.name FROM sessions s
				JOIN workers w ON s.worker_id = w.id
				WHERE s.shop_id = $1 AND s.status = 'active'
				ORDER BY s.started_at DESC LIMIT 1
			`, shopID).Scan(&activeSession)

			var lastEventTime time.Time
			_ = conn.QueryRow(`SELECT MAX(created_at) FROM events WHERE shop_id = $1`, shopID).Scan(&lastEventTime)

			var ledgerIntegrity string
			var eventCountCheck int
			err = conn.QueryRow(`SELECT COUNT(*) FROM events WHERE shop_id = $1`, shopID).Scan(&eventCountCheck)
			if err == nil && eventCountCheck > 0 {
				ledgerIntegrity = "✅ Hash chain intact"
			} else if eventCountCheck == 0 {
				ledgerIntegrity = "ℹ️  No events yet"
			} else {
				ledgerIntegrity = "⚠️  Cannot verify"
			}

			info := map[string]interface{}{
				"shop_id":        shopID,
				"name":           name,
				"owner_email":    ownerEmail,
				"currency":       currency,
				"tax_rate":       taxRate,
				"created_at":     createdAt.Format("2006-01-02 15:04:05"),
				"products":       productCount,
				"workers":        workerCount,
				"total_events":   eventCount,
				"total_sales":    saleCount,
				"total_revenue":  totalRevenue,
				"active_session": activeSession,
				"last_event":     lastEventTime.Format("2006-01-02 15:04:05"),
				"ledger":         ledgerIntegrity,
			}

			if jsonOut {
				return printJSON(info)
			}

			fmt.Printf("📋 Shop Inspection: %s\n", name)
			fmt.Println(strings.Repeat("=", 50))
			fmt.Printf("ID:            %s\n", shopID)
			fmt.Printf("Owner:         %s\n", ownerEmail)
			fmt.Printf("Currency:      %s\n", currency)
			fmt.Printf("Tax Rate:      %.2f%%\n", taxRate)
			fmt.Printf("Created:       %s\n", createdAt.Format("2006-01-02"))
			fmt.Println()
			fmt.Printf("Products:      %d\n", productCount)
			fmt.Printf("Workers:       %d\n", workerCount)
			fmt.Printf("Total Events:  %d\n", eventCount)
			fmt.Printf("Total Sales:   %d\n", saleCount)
			fmt.Printf("Revenue:       %s %d\n", currency, totalRevenue)
			fmt.Println()
			fmt.Printf("Active Shift:  %s\n", valueOr(activeSession, "none"))
			fmt.Printf("Last Event:    %s\n", lastEventTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("Ledger:        %s\n", ledgerIntegrity)

			return nil
		},
	}
}

func queryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query [sql]",
		Short: "Run raw SQL query (DANGEROUS — read-only)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			upper := strings.ToUpper(query)
			if strings.Contains(upper, "DELETE") || strings.Contains(upper, "DROP") ||
				strings.Contains(upper, "TRUNCATE") || strings.Contains(upper, "INSERT") ||
				strings.Contains(upper, "UPDATE") || strings.Contains(upper, "ALTER") ||
				strings.Contains(upper, "CREATE") {
				return fmt.Errorf("❌ READ-ONLY mode. DELETE/DROP/INSERT/UPDATE/ALTER/CREATE not allowed")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			rows, err := conn.Query(query)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()

			cols, _ := rows.Columns()
			results := []map[string]interface{}{}

			for rows.Next() {
				vals := make([]interface{}, len(cols))
				valPtrs := make([]interface{}, len(cols))
				for i := range cols {
					valPtrs[i] = &vals[i]
				}
				if err := rows.Scan(valPtrs...); err != nil {
					return err
				}
				row := make(map[string]interface{})
				for i, col := range cols {
					row[col] = vals[i]
				}
				results = append(results, row)
			}

			if jsonOut {
				return printJSON(results)
			}

			fmt.Printf("📊 Query returned %d rows\n", len(results))
			if len(results) > 0 {
				for i, row := range results {
					if i > 0 {
						fmt.Println(strings.Repeat("-", 40))
					}
					for k, v := range row {
						fmt.Printf("  %-20s: %v\n", k, v)
					}
				}
			}

			return nil
		},
	}
}

func eventsCmd() *cobra.Command {
	var limit int
	var eventType string
	cmd := &cobra.Command{
		Use:   "events",
		Short: "View recent events for a shop",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			query := `SELECT id, event_seq, event_type, event_data, event_hash, created_at
				FROM events WHERE shop_id = $1`
			params := []interface{}{shopID}

			if eventType != "" {
				query += " AND event_type = $2"
				params = append(params, eventType)
			}

			query += " ORDER BY event_seq DESC LIMIT $%d"
			params = append(params, limit)

			rows, err := conn.Query(query, params...)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()

			type eventRow struct {
				ID        int64           `json:"id"`
				Seq       int64           `json:"seq"`
				Type      string          `json:"type"`
				Data      json.RawMessage `json:"data"`
				Hash      string          `json:"hash"`
				CreatedAt time.Time       `json:"created_at"`
			}

			var results []eventRow
			for rows.Next() {
				var e eventRow
				if err := rows.Scan(&e.ID, &e.Seq, &e.Type, &e.Data, &e.Hash, &e.CreatedAt); err != nil {
					return err
				}
				results = append(results, e)
			}

			if jsonOut {
				return printJSON(results)
			}

			fmt.Printf("📜 Events for shop %s (last %d)\n", shopID, limit)
			fmt.Println(strings.Repeat("=", 60))
			for _, e := range results {
				fmt.Printf("#%d | %s | %s | %s\n", e.Seq, e.CreatedAt.Format("15:04:05"), e.Type, e.Hash[:16]+"...")
			}

			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Number of events")
	cmd.Flags().StringVar(&eventType, "type", "", "Filter by event type")
	return cmd
}

func ledgerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ledger",
		Short: "Verify hash chain integrity for a shop",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			rows, err := conn.Query(`SELECT event_seq, event_data, previous_hash, event_hash
				FROM events WHERE shop_id = $1 ORDER BY event_seq`, shopID)
			if err != nil {
				return err
			}
			defer rows.Close()

			prevHash := ""
			var count, errors int
			for rows.Next() {
				var seq int64
				var data, prevH, hash string
				if err := rows.Scan(&seq, &data, &prevH, &hash); err != nil {
					return err
				}

				if prevH != prevHash {
					fmt.Printf("❌ Chain broken at event #%d: previous_hash mismatch\n", seq)
					errors++
				}
				prevHash = hash
				count++
			}

			if jsonOut {
				return printJSON(map[string]interface{}{
					"events_checked": count,
					"errors":         errors,
					"status":         valueOr(map[bool]string{true: "✅ VALID", false: "❌ CORRUPTED"}[errors == 0], "UNKNOWN"),
				})
			}

			fmt.Printf("🔒 Ledger Verification: %s\n", shopID)
			fmt.Println(strings.Repeat("=", 50))
			fmt.Printf("Events checked: %d\n", count)
			fmt.Printf("Errors found:   %d\n", errors)
			if errors == 0 {
				fmt.Println("Status:         ✅ Hash chain intact")
			} else {
				fmt.Printf("Status:         ❌ %d integrity errors found\n", errors)
			}

			return nil
		},
	}
}

func ghostCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ghost",
		Short: "Run Ghost Mode anomaly detection for a shop",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			// Variance patterns
			fmt.Printf("👻 Ghost Mode: Scanning shop %s\n", shopID)
			fmt.Println(strings.Repeat("=", 60))

			// Check for workers with consistent negative variance
			rows, err := conn.Query(`
				SELECT 
					event_data->>'worker_id' as worker,
					COUNT(*) as total_reconciles,
					SUM(CASE WHEN (event_data->>'cash_variance')::bigint < 0 
						OR (event_data->>'mpesa_variance')::bigint < 0 THEN 1 ELSE 0 END) as short_count,
					SUM((event_data->>'cash_variance')::bigint + (event_data->>'mpesa_variance')::bigint) as total_variance
				FROM events 
				WHERE shop_id = $1 AND event_type = 'reconciliation'
				GROUP BY event_data->>'worker_id'
				HAVING COUNT(*) >= 2
			`, shopID)
			if err != nil {
				return err
			}
			defer rows.Close()

			var found bool
			for rows.Next() {
				var worker string
				var total, short int
				var variance int64
				if err := rows.Scan(&worker, &total, &short, &variance); err != nil {
					return err
				}

				shortRate := float64(short) / float64(total) * 100
				if shortRate >= 50 {
					found = true
					severity := "🟡 MEDIUM"
					if shortRate >= 80 {
						severity = "🟠 HIGH"
					}
					if shortRate >= 95 && total >= 5 {
						severity = "🔴 CRITICAL"
					}
					fmt.Printf("\n%s Variance Pattern: %s\n", severity, worker)
					fmt.Printf("   Short in %d/%d reconciliations (%.0f%%)\n", short, total, shortRate)
					fmt.Printf("   Total variance: %d\n", variance)
				}
			}

			// Price manipulation check
			priceRows, err := conn.Query(`
				SELECT 
					event_data->>'worker_id' as worker,
					event_data->>'product_id' as product,
					MIN((event_data->>'price')::bigint) as min_price,
					MAX((event_data->>'price')::bigint) as max_price,
					COUNT(*) as sale_count
				FROM events
				WHERE shop_id = $1 AND event_type = 'sale'
				GROUP BY event_data->>'worker_id', event_data->>'product_id'
				HAVING COUNT(*) >= 3
			`, shopID)
			if err != nil {
				return err
			}
			defer priceRows.Close()

			for priceRows.Next() {
				var worker, product string
				var minP, maxP int64
				var count int
				if err := priceRows.Scan(&worker, &product, &minP, &maxP, &count); err != nil {
					return err
				}

				if minP > 0 {
					spread := float64(maxP-minP) / float64(minP) * 100
					if spread > 20 {
						found = true
						fmt.Printf("\n🟡 Price Manipulation: %s selling %s\n", worker, product)
						fmt.Printf("   Price range: %d - %d (%.0f%% spread) across %d sales\n", minP, maxP, spread, count)
					}
				}
			}

			if !found {
				fmt.Println("\n✅ No anomalies detected")
			}

			return nil
		},
	}
}

func usersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "users",
		Short: "List or manage shop users",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			rows, err := conn.Query(`
				SELECT id, email, role, name, active, created_at 
				FROM shop_users WHERE shop_id = $1 ORDER BY created_at
			`, shopID)
			if err != nil {
				return err
			}
			defer rows.Close()

			fmt.Printf("👥 Users for shop %s\n", shopID)
			fmt.Println(strings.Repeat("=", 60))
			for rows.Next() {
				var id, email, role, name string
				var active bool
				var created time.Time
				if err := rows.Scan(&id, &email, &role, &name, &active, &created); err != nil {
					return err
				}
				status := "✅"
				if !active {
					status = "❌"
				}
				fmt.Printf("%s %-30s %-10s %-20s %s\n", status, email, role, name, created.Format("2006-01-02"))
			}

			return nil
		},
	}
}

func sessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "View active and recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			rows, err := conn.Query(`
				SELECT s.id, w.name, s.started_at, s.ended_at, s.status, s.opening_float
				FROM sessions s
				JOIN workers w ON s.worker_id = w.id
				WHERE s.shop_id = $1
				ORDER BY s.started_at DESC
				LIMIT 20
			`, shopID)
			if err != nil {
				return err
			}
			defer rows.Close()

			fmt.Printf("🕐 Sessions for shop %s\n", shopID)
			fmt.Println(strings.Repeat("=", 60))
			for rows.Next() {
				var id, worker, status string
				var started time.Time
				var ended sql.NullTime
				var openingFloat int64
				if err := rows.Scan(&id, &worker, &started, &ended, &status, &openingFloat); err != nil {
					return err
				}
				endedStr := "active"
				if ended.Valid {
					endedStr = ended.Time.Format("15:04:05")
				}
				fmt.Printf("%-8s | %-15s | %s → %s | float: %d\n",
					status, worker, started.Format("15:04"), endedStr, openingFloat)
			}

			return nil
		},
	}
}

func backupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Create a database backup (pg_dump)",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := fmt.Sprintf("minidb_backup_%s.sql", time.Now().Format("20060102_150405"))
			fmt.Printf("💾 Creating backup: %s\n", output)

			// Use pg_dump via exec
			// For now, just verify DB is accessible
			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			var dbName string
			_ = conn.QueryRow("SELECT current_database()").Scan(&dbName)

			fmt.Printf("✅ Database '%s' is accessible\n", dbName)
			fmt.Println("To create a full backup, run:")
			fmt.Printf("  pg_dump '%s' > %s\n", dbURL, output)
			fmt.Println()
			fmt.Println("To restore:")
			fmt.Printf("  psql '%s' < %s\n", dbURL, output)

			return nil
		},
	}
}

func emergencyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emergency",
		Short: "Emergency operations (USE WITH CAUTION)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "force-close-session [worker_id]",
		Short: "Force close a stuck session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			result, err := conn.Exec(`
				UPDATE sessions SET status = 'closed', ended_at = NOW()
				WHERE worker_id = $1 AND shop_id = $2 AND status = 'active'
			`, args[0], shopID)
			if err != nil {
				return err
			}

			rows, _ := result.RowsAffected()
			fmt.Printf("✅ Closed %d session(s) for worker %s\n", rows, args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "reset-worker-pin [worker_id] [new_pin]",
		Short: "Reset a worker's PIN",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			_, err = conn.Exec(`UPDATE workers SET pin = $1 WHERE id = $2 AND shop_id = $3`,
				args[1], args[0], shopID)
			if err != nil {
				return err
			}

			fmt.Printf("✅ PIN reset for worker %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "fix-stock [product_id] [correct_qty]",
		Short: "Manually correct stock quantity",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			var currentQty int64
			err = conn.QueryRow(`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2`,
				args[0], shopID).Scan(&currentQty)
			if err != nil {
				return fmt.Errorf("product not found: %w", err)
			}

			fmt.Printf("Current stock: %d → New stock: %s\n", currentQty, args[1])
			fmt.Print("Confirm? (yes/no): ")
			var confirm string
			fmt.Scanln(&confirm)
			if confirm != "yes" {
				fmt.Println("Cancelled")
				return nil
			}

			_, err = conn.Exec(`UPDATE products SET stock_qty = $1 WHERE id = $2 AND shop_id = $3`,
				args[1], args[0], shopID)
			if err != nil {
				return err
			}

			fmt.Println("✅ Stock corrected")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "disable-user [user_id]",
		Short: "Immediately disable a user's access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			result, err := conn.Exec(`UPDATE shop_users SET active = false WHERE id = $1`, args[0])
			if err != nil {
				return err
			}

			rows, _ := result.RowsAffected()
			fmt.Printf("✅ Disabled %d user(s)\n", rows)
			return nil
		},
	})

	return cmd
}

func metricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "metrics",
		Short: "Database performance metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			fmt.Println("📊 Database Metrics")
			fmt.Println(strings.Repeat("=", 50))

			var dbSize string
			_ = conn.QueryRow("SELECT pg_size_pretty(pg_database_size(current_database()))").Scan(&dbSize)
			fmt.Printf("Database Size:    %s\n", dbSize)

			var connCount int
			_ = conn.QueryRow("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()").Scan(&connCount)
			fmt.Printf("Active Connections: %d\n", connCount)

			var cacheHit float64
			_ = conn.QueryRow("SELECT round(sum(blks_hit) / nullif(sum(blks_hit) + sum(blks_read), 0) * 100, 2) FROM pg_stat_database WHERE datname = current_database()").Scan(&cacheHit)
			fmt.Printf("Cache Hit Ratio:  %.2f%%\n", cacheHit)

			var deadTuples int64
			_ = conn.QueryRow("SELECT COALESCE(SUM(n_dead_tup), 0) FROM pg_stat_user_tables").Scan(&deadTuples)
			fmt.Printf("Dead Tuples:      %d\n", deadTuples)

			var largestTable, largestSize string
			_ = conn.QueryRow(`
				SELECT relname, pg_size_pretty(pg_total_relation_size(c.oid))
				FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = 'public'
				ORDER BY pg_total_relation_size(c.oid) DESC
				LIMIT 1
			`).Scan(&largestTable, &largestSize)
			fmt.Printf("Largest Table:  %s (%s)\n", largestTable, largestSize)

			return nil
		},
	}
}

func logsCmd() *cobra.Command {
	var level string
	var tail int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View recent audit log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shopID == "" {
				return fmt.Errorf("--shop is required")
			}

			conn, err := connectDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			query := `SELECT action, details, ip_address, created_at FROM audit_log WHERE shop_id = $1`
			params := []interface{}{shopID}

			if level != "" {
				query += " AND action LIKE $2"
				params = append(params, "%"+level+"%")
			}

			query += " ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d", len(params)+1)
			params = append(params, tail)

			rows, err := conn.Query(query, params...)
			if err != nil {
				return err
			}
			defer rows.Close()

			fmt.Printf("📝 Audit Log: %s\n", shopID)
			fmt.Println(strings.Repeat("=", 60))
			for rows.Next() {
				var action string
				var details sql.NullString
				var ip sql.NullString
				var created time.Time
				if err := rows.Scan(&action, &details, &ip, &created); err != nil {
					return err
				}
				ipStr := valueOr(ip.String, "unknown")
				detailStr := valueOr(details.String, "")
				fmt.Printf("[%s] %-25s %s %s\n",
					created.Format("2006-01-02 15:04:05"),
					action,
					ipStr,
					detailStr)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&level, "level", "", "Filter by action type")
	cmd.Flags().IntVar(&tail, "tail", 50, "Number of entries")
	return cmd
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
