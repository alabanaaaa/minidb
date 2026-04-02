package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"mini-database/engine"
	"mini-database/internal/auth"
	"mini-database/internal/db"
	"mini-database/internal/mpesa"
	"mini-database/internal/receipt"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	db          *db.DB
	templates   map[string]*template.Template
	authSvc     *auth.Service
	mpesaClient *mpesa.Client
	engine      *engine.Engine
}

func NewHandler(database *db.DB, authSvc *auth.Service, mpesaClient *mpesa.Client, eng *engine.Engine) *Handler {
	h := &Handler{
		db:          database,
		templates:   make(map[string]*template.Template),
		authSvc:     authSvc,
		mpesaClient: mpesaClient,
		engine:      eng,
	}
	h.loadTemplates()
	return h
}

func (h *Handler) loadTemplates() {
	// Load standalone pages (login, signup)
	for _, page := range []string{"login", "signup"} {
		path := fmt.Sprintf("web/templates/pages/%s.html", page)
		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("failed to read template", "page", page, "error", err)
			continue
		}
		tmpl, err := template.New(page).Parse(string(content))
		if err != nil {
			slog.Warn("failed to parse template", "page", page, "error", err)
			continue
		}
		h.templates[page] = tmpl
	}

	// Load layout + block pages
	layoutContent, err := os.ReadFile("web/templates/layouts/base.html")
	if err != nil {
		slog.Error("failed to read layout", "error", err)
		return
	}

	for _, page := range []string{"dashboard", "pos", "inventory", "workers", "sessions", "reports", "forecast", "users", "settings"} {
		pagePath := fmt.Sprintf("web/templates/pages/%s.html", page)
		pageContent, err := os.ReadFile(pagePath)
		if err != nil {
			slog.Warn("failed to read page template", "page", page, "error", err)
			continue
		}
		combined := string(layoutContent) + "\n" + string(pageContent)
		tmpl, err := template.New(page).Parse(combined)
		if err != nil {
			slog.Warn("failed to parse combined template", "page", page, "error", err)
			continue
		}
		h.templates[page] = tmpl
	}
}

func (h *Handler) RegisterRoutes(mux *chi.Mux) {
	mux.HandleFunc("GET /login", h.handleLoginGET)
	mux.HandleFunc("POST /login", h.handleLoginPOST)
	mux.HandleFunc("GET /signup", h.handleSignupGET)
	mux.HandleFunc("POST /signup", h.handleSignupPOST)
	mux.HandleFunc("GET /logout", h.handleLogout)

	mux.HandleFunc("GET /dashboard", h.requireAuth(h.handleDashboard))
	mux.HandleFunc("GET /pos", h.requireAuth(h.handlePOS))
	mux.HandleFunc("POST /pos/checkout", h.requireAuth(h.handleCheckout))
	mux.HandleFunc("GET /inventory", h.requireAuth(h.handleInventory))
	mux.HandleFunc("POST /inventory/add", h.requireAuth(h.handleAddProduct))
	mux.HandleFunc("GET /workers", h.requireAuth(h.handleWorkers))
	mux.HandleFunc("POST /workers/add", h.requireAuth(h.handleAddWorker))
	mux.HandleFunc("GET /sessions", h.requireAuth(h.handleSessions))
	mux.HandleFunc("POST /sessions/open", h.requireAuth(h.handleOpenSession))
	mux.HandleFunc("POST /sessions/{id}/close", h.requireAuth(h.handleCloseSession))
	mux.HandleFunc("GET /reports", h.requireAuth(h.handleReports))
	mux.HandleFunc("GET /forecast", h.requireAuth(h.handleForecast))
	mux.HandleFunc("GET /admin/users", h.requireAuth(h.requireOwner(h.handleUsers)))
	mux.HandleFunc("POST /admin/users/add", h.requireAuth(h.requireOwner(h.handleAddUser)))
	mux.HandleFunc("POST /admin/users/{id}/toggle", h.requireAuth(h.requireOwner(h.handleToggleUser)))
	mux.HandleFunc("POST /admin/users/{id}/role", h.requireAuth(h.requireOwner(h.handleUserRole)))
	mux.HandleFunc("GET /settings", h.requireAuth(h.handleSettings))
	mux.HandleFunc("POST /settings/shop", h.requireAuth(h.handleSettingsShop))
	mux.HandleFunc("POST /settings/mpesa", h.requireAuth(h.handleSettingsMpesa))
	mux.HandleFunc("POST /settings/email", h.requireAuth(h.handleSettingsEmail))
	mux.HandleFunc("GET /export/sales", h.requireAuth(h.handleExportSales))
	mux.HandleFunc("GET /export/inventory", h.requireAuth(h.handleExportInventory))
	mux.HandleFunc("GET /export/workers", h.requireAuth(h.handleExportWorkers))
	mux.HandleFunc("GET /receipt/{id}", h.requireAuth(h.handleReceipt))
	mux.HandleFunc("GET /api/charts/sales-trend", h.requireAuth(h.handleChartSalesTrend))
	mux.HandleFunc("GET /api/charts/payment-methods", h.requireAuth(h.handleChartPaymentMethods))
	mux.HandleFunc("GET /api/charts/top-products", h.requireAuth(h.handleChartTopProducts))
	mux.HandleFunc("GET /api/forecast/revenue-trend", h.requireAuth(h.handleForecastRevenueTrend))
	mux.HandleFunc("GET /api/forecast/product-radar", h.requireAuth(h.handleForecastProductRadar))
	mux.HandleFunc("GET /api/forecast/daily-pattern", h.requireAuth(h.handleForecastDailyPattern))
	mux.HandleFunc("GET /api/forecast/stock-health", h.requireAuth(h.handleForecastStockHealth))
	mux.HandleFunc("GET /api/reports/summary", h.requireAuth(h.handleReportSummary))
	mux.HandleFunc("GET /api/reports/transactions", h.requireAuth(h.handleReportTransactions))
	mux.HandleFunc("GET /api/reports/workers", h.requireAuth(h.handleReportWorkers))
	mux.HandleFunc("GET /api/reports/products", h.requireAuth(h.handleReportProducts))
	mux.HandleFunc("GET /api/reports/hourly", h.requireAuth(h.handleReportHourly))
	mux.HandleFunc("POST /api/sync", h.requireAuth(h.handleSyncOfflineSales))
	mux.HandleFunc("POST /mpesa/callback", h.handleMpesaCallback)
}

type pageData struct {
	Page         string
	Title        string
	UserName     string
	UserRole     string
	UserInitials string
	Currency     string
}

func (h *Handler) getUserContext(r *http.Request, page, title string) pageData {
	name, _ := r.Context().Value("user_name").(string)
	role, _ := r.Context().Value("user_role").(string)
	currency, _ := r.Context().Value("currency").(string)

	if currency == "" {
		currency = "KES"
	}

	initials := "?"
	if len(name) > 0 {
		parts := strings.Fields(name)
		if len(parts) >= 2 {
			initials = string(parts[0][0]) + string(parts[1][0])
		} else {
			initials = strings.ToUpper(string(name[0]))
		}
	}

	return pageData{
		Page:         page,
		Title:        title,
		UserName:     name,
		UserRole:     role,
		UserInitials: initials,
		Currency:     currency,
	}
}

func (h *Handler) handleLoginGET(w http.ResponseWriter, r *http.Request) {
	h.templates["login"].Execute(w, nil)
}

func (h *Handler) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	var userID, shopID, userName, userRole, passwordHash, currency string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT su.id, su.shop_id, su.name, su.role, su.password_hash, s.currency
		FROM shop_users su JOIN shops s ON su.shop_id = s.id
		WHERE su.email = $1 AND su.active = true
	`, email).Scan(&userID, &shopID, &userName, &userRole, &passwordHash, &currency)

	if err == sql.ErrNoRows || auth.CheckPassword(passwordHash, password) != nil {
		h.templates["login"].Execute(w, map[string]string{"Error": "Invalid email or password"})
		return
	}

	if err != nil {
		slog.Error("login query failed", "error", err)
		h.templates["login"].Execute(w, map[string]string{"Error": "Internal error"})
		return
	}

	access, refresh, err := h.authSvc.GenerateToken(userID, shopID, email, userRole)
	if err != nil {
		slog.Error("token generation failed", "error", err)
		h.templates["login"].Execute(w, map[string]string{"Error": "Internal error"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    access,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refresh,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   604800,
	})

	http.SetCookie(w, &http.Cookie{
		Name:   "user_name",
		Value:  userName,
		Path:   "/",
		MaxAge: 86400,
	})

	http.SetCookie(w, &http.Cookie{
		Name:   "user_role",
		Value:  userRole,
		Path:   "/",
		MaxAge: 86400,
	})

	http.SetCookie(w, &http.Cookie{
		Name:   "currency",
		Value:  currency,
		Path:   "/",
		MaxAge: 86400,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handler) handleSignupGET(w http.ResponseWriter, r *http.Request) {
	h.templates["signup"].Execute(w, nil)
}

func (h *Handler) handleSignupPOST(w http.ResponseWriter, r *http.Request) {
	shopName := r.FormValue("shop_name")
	currency := r.FormValue("currency")
	ownerName := r.FormValue("name")
	email := r.FormValue("email")
	password := r.FormValue("password")

	if shopName == "" || ownerName == "" || email == "" || password == "" {
		h.templates["signup"].Execute(w, map[string]string{"Error": "All fields are required"})
		return
	}

	if len(password) < 6 {
		h.templates["signup"].Execute(w, map[string]string{"Error": "Password must be at least 6 characters"})
		return
	}

	if currency == "" {
		currency = "KES"
	}

	err := h.db.Transactional(r.Context(), func(tx *sql.Tx) error {
		var shopID string
		err := tx.QueryRowContext(r.Context(), `
			INSERT INTO shops (name, owner_email, currency) VALUES ($1, $2, $3) RETURNING id
		`, shopName, email, currency).Scan(&shopID)
		if err != nil {
			return fmt.Errorf("create shop: %w", err)
		}

		hash, err := auth.HashPassword(password)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}

		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO shop_users (shop_id, email, password_hash, role, name)
			VALUES ($1, $2, $3, 'owner', $4)
		`, shopID, email, hash, ownerName)
		if err != nil {
			return fmt.Errorf("create owner user: %w", err)
		}

		return nil
	})

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			h.templates["signup"].Execute(w, map[string]string{"Error": "An account with this email already exists"})
			return
		}
		slog.Error("signup failed", "error", err)
		h.templates["signup"].Execute(w, map[string]string{"Error": "Failed to create account. Please try again."})
		return
	}

	h.templates["signup"].Execute(w, map[string]string{
		"Success": "Shop created! You can now sign in.",
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{"access_token", "refresh_token", "user_name", "user_role", "currency"} {
		http.SetCookie(w, &http.Cookie{
			Name:   name,
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("access_token")
		if err != nil || cookie.Value == "" {
			slog.Info("requireAuth: no access token", "error", err)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		claims, err := h.authSvc.VerifyToken(cookie.Value)
		if err != nil {
			slog.Info("requireAuth: invalid token", "error", err)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var userName, currency string
		if c, _ := r.Cookie("user_name"); c != nil {
			userName = c.Value
		}
		if c, _ := r.Cookie("currency"); c != nil {
			currency = c.Value
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, contextKey("user_role"), claims.Role)
		ctx = context.WithValue(ctx, contextKey("user_name"), userName)
		ctx = context.WithValue(ctx, contextKey("currency"), currency)
		ctx = context.WithValue(ctx, contextKey("shop_id"), claims.ShopID)
		ctx = context.WithValue(ctx, contextKey("user_id"), claims.UserID)

		slog.Info("requireAuth: calling next", "role", claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

type contextKey string

func addContextValue(ctx context.Context, key, value string) context.Context {
	return context.WithValue(ctx, contextKey(key), value)
}

func getContextValue(r *http.Request, key string) string {
	v, _ := r.Context().Value(contextKey(key)).(string)
	return v
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "dashboard", "Dashboard")

	var todaySales, todayCash, todayMpesa int64
	var todaySaleCount, lowStock, activeSessions int

	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0), COUNT(*)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todaySales, &todaySaleCount)

	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND (event_data->>'payment')::int = 1 AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todayCash)

	todayMpesa = todaySales - todayCash

	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty <= min_stock AND active = true
	`, shopID).Scan(&lowStock)

	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM sessions WHERE shop_id = $1 AND status = 'active'
	`, shopID).Scan(&activeSessions)

	type recentSale struct {
		ProductID string
		Quantity  int64
		Total     int64
		WorkerID  string
	}

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT event_data->>'product_id', (event_data->>'quantity')::bigint,
			(event_data->>'price')::bigint * (event_data->>'quantity')::bigint,
			event_data->>'worker_id'
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		ORDER BY created_at DESC LIMIT 10
	`, shopID)
	defer rows.Close()

	var recentSales []recentSale
	for rows.Next() {
		var s recentSale
		rows.Scan(&s.ProductID, &s.Quantity, &s.Total, &s.WorkerID)
		recentSales = append(recentSales, s)
	}

	riskScore := 0
	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM events WHERE shop_id = $1 AND event_type = 'reconciliation'
		AND ((event_data->>'cash_variance')::bigint < 0 OR (event_data->>'mpesa_variance')::bigint < 0)
	`, shopID).Scan(&riskScore)
	riskScore = min(riskScore*10, 100)

	alerts := h.engine.GetLowStockAlerts()

	avgTransaction := int64(0)
	if todaySaleCount > 0 {
		avgTransaction = todaySales / int64(todaySaleCount)
	}
	cashPct := "0"
	mpesaPct := "0"
	if todaySales > 0 {
		cashPct = fmt.Sprintf("%.0f", float64(todayCash)/float64(todaySales)*100)
		mpesaPct = fmt.Sprintf("%.0f", float64(todayMpesa)/float64(todaySales)*100)
	}

	data := struct {
		pageData
		TodaySales         string
		TodayCash          string
		TodayMpesa         string
		TodaySaleCount     int
		AvgTransaction     string
		CashPct            string
		MpesaPct           string
		LowStock           int
		LowStockAlertsList []engine.LowStockAlert
		ActiveSessions     int
		RiskScore          int
		RecentSales        []recentSale
		Anomalies          []map[string]string
	}{
		pageData:           userCtx,
		TodaySales:         fmt.Sprintf("%d", todaySales),
		TodayCash:          fmt.Sprintf("%d", todayCash),
		TodayMpesa:         fmt.Sprintf("%d", todayMpesa),
		TodaySaleCount:     todaySaleCount,
		AvgTransaction:     fmt.Sprintf("%d", avgTransaction),
		CashPct:            cashPct,
		MpesaPct:           mpesaPct,
		LowStock:           lowStock,
		LowStockAlertsList: alerts,
		ActiveSessions:     activeSessions,
		RiskScore:          riskScore,
		RecentSales:        recentSales,
	}

	if err := h.templates["dashboard"].Execute(w, data); err != nil {
		slog.Error("dashboard template failed", "error", err)
		http.Error(w, "Template error: "+err.Error(), 500)
		return
	}
}

func (h *Handler) handlePOS(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "pos", "Point of Sale")

	type product struct {
		ID        string
		Name      string
		SKU       string
		SellPrice int64
		StockQty  int64
	}

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT id, name, COALESCE(sku, ''), sell_price, stock_qty FROM products WHERE shop_id = $1 AND active = true ORDER BY name
	`, shopID)
	defer rows.Close()

	var products []product
	for rows.Next() {
		var p product
		rows.Scan(&p.ID, &p.Name, &p.SKU, &p.SellPrice, &p.StockQty)
		products = append(products, p)
	}

	data := struct {
		pageData
		Products []product
		WorkerID string
		Success  string
	}{
		pageData: userCtx,
		Products: products,
		WorkerID: getContextValue(r, "user_id"),
		Success:  r.URL.Query().Get("success"),
	}

	h.templates["pos"].Execute(w, data)
}

func (h *Handler) handleCheckout(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userID := getContextValue(r, "user_id")

	cartJSON := r.FormValue("cart")
	payment := r.FormValue("payment")
	workerID := r.FormValue("worker_id")
	phone := r.FormValue("phone")

	var items []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Price int64  `json:"price"`
		Qty   int64  `json:"qty"`
	}
	if err := json.Unmarshal([]byte(cartJSON), &items); err != nil || len(items) == 0 {
		http.Redirect(w, r, "/pos", http.StatusSeeOther)
		return
	}

	var total int64
	for _, item := range items {
		total += item.Price * item.Qty
	}

	paymentInt := 1
	if payment == "2" {
		paymentInt = 2
	}

	err := h.db.Transactional(r.Context(), func(tx *sql.Tx) error {
		for _, item := range items {
			var currentStock int64
			err := tx.QueryRowContext(r.Context(),
				`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2 AND active = true`,
				item.ID, shopID,
			).Scan(&currentStock)
			if err != nil {
				return fmt.Errorf("product %s not found", item.Name)
			}
			if currentStock < item.Qty {
				return fmt.Errorf("insufficient stock for %s", item.Name)
			}

			_, err = tx.ExecContext(r.Context(),
				`UPDATE products SET stock_qty = stock_qty - $1 WHERE id = $2 AND shop_id = $3`,
				item.Qty, item.ID, shopID,
			)
			if err != nil {
				return err
			}

			eventData, _ := json.Marshal(map[string]interface{}{
				"product_id": item.ID,
				"quantity":   item.Qty,
				"price":      item.Price,
				"worker_id":  workerID,
				"payment":    paymentInt,
				"total":      item.Price * item.Qty,
			})

			var lastHash string
			_ = tx.QueryRowContext(r.Context(),
				`SELECT COALESCE(event_hash, '') FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`,
				shopID,
			).Scan(&lastHash)

			var seq int64
			_ = tx.QueryRowContext(r.Context(),
				`SELECT COALESCE(MAX(event_seq), 0) FROM events WHERE shop_id = $1`,
				shopID,
			).Scan(&seq)
			seq++

			content := string(eventData) + lastHash
			sum := sha256.Sum256([]byte(content))
			eventHash := hex.EncodeToString(sum[:])

			_, err = tx.ExecContext(r.Context(), `
				INSERT INTO events (shop_id, event_seq, event_type, event_data, previous_hash, event_hash, created_by)
				VALUES ($1, $2, 'sale', $3, $4, $5, $6)
			`, shopID, seq, string(eventData), lastHash, eventHash, userID)
			if err != nil {
				return err
			}
		}

		if paymentInt == 2 && h.mpesaClient != nil && phone != "" {
			accountRef := mpesa.GenerateAccountRef()
			_, mpErr := tx.ExecContext(r.Context(), `
				INSERT INTO mpesa_transactions (shop_id, mpesa_receipt, phone_number, amount, status)
				VALUES ($1, $2, $3, $4, 'pending')
			`, shopID, accountRef, phone, total)
			if mpErr != nil {
				slog.Warn("failed to store mpesa transaction", "error", mpErr)
			}

			stkResp, stkErr := h.mpesaClient.STKPush(mpesa.STKPushRequest{
				PhoneNumber: phone,
				Amount:      total,
				AccountRef:  accountRef,
				Description: fmt.Sprintf("Payment for %d items", len(items)),
			})
			if stkErr != nil {
				slog.Error("stk push failed", "error", stkErr)
			} else {
				slog.Info("stk push initiated", "checkout_request_id", stkResp.CheckoutRequestID, "amount", total)
			}
		}

		return nil
	})

	if err != nil {
		slog.Error("checkout failed", "error", err)
		http.Redirect(w, r, "/pos", http.StatusSeeOther)
		return
	}

	if paymentInt == 2 {
		http.Redirect(w, r, fmt.Sprintf("/pos?success=Sale+recorded.+M-Pesa+prompt+sent+to+%s", phone), http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/pos?success=Sale+completed", http.StatusSeeOther)
	}
}

func (h *Handler) handleInventory(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "inventory", "Inventory")

	type product struct {
		ID        string
		Name      string
		SKU       string
		CostPrice int64
		SellPrice int64
		StockQty  int64
		MinStock  int64
	}

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT id, name, COALESCE(sku,''), cost_price, sell_price, stock_qty, min_stock
		FROM products WHERE shop_id = $1 AND active = true ORDER BY name
	`, shopID)
	defer rows.Close()

	var products []product
	for rows.Next() {
		var p product
		rows.Scan(&p.ID, &p.Name, &p.SKU, &p.CostPrice, &p.SellPrice, &p.StockQty, &p.MinStock)
		products = append(products, p)
	}

	h.templates["inventory"].Execute(w, struct {
		pageData
		Products []product
	}{
		pageData: userCtx,
		Products: products,
	})
}

func (h *Handler) handleAddProduct(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	name := r.FormValue("name")
	sku := r.FormValue("sku")
	costPrice := r.FormValue("cost_price")
	sellPrice := r.FormValue("sell_price")
	stockQty := r.FormValue("stock_qty")
	minStock := r.FormValue("min_stock")

	if minStock == "" {
		minStock = "5"
	}

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO products (shop_id, name, sku, cost_price, sell_price, stock_qty, min_stock)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, shopID, name, sku, costPrice, sellPrice, stockQty, minStock)

	if err != nil {
		slog.Error("add product failed", "error", err)
	}

	http.Redirect(w, r, "/inventory", http.StatusSeeOther)
}

func (h *Handler) handleWorkers(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "workers", "Workers")

	type worker struct {
		Name      string
		HasPIN    bool
		Active    bool
		CreatedAt string
	}

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT name, pin IS NOT NULL, active, TO_CHAR(created_at, 'YYYY-MM-DD')
		FROM workers WHERE shop_id = $1 ORDER BY name
	`, shopID)
	defer rows.Close()

	var workers []worker
	for rows.Next() {
		var wk worker
		rows.Scan(&wk.Name, &wk.HasPIN, &wk.Active, &wk.CreatedAt)
		workers = append(workers, wk)
	}

	h.templates["workers"].Execute(w, struct {
		pageData
		Workers []worker
	}{
		pageData: userCtx,
		Workers:  workers,
	})
}

func (h *Handler) handleAddWorker(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	name := r.FormValue("name")
	pin := r.FormValue("pin")

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO workers (shop_id, name, pin) VALUES ($1, $2, $3)
	`, shopID, name, pin)

	if err != nil {
		slog.Error("add worker failed", "error", err)
	}

	http.Redirect(w, r, "/workers", http.StatusSeeOther)
}

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "sessions", "Sessions")

	type session struct {
		ID           string
		Worker       string
		StartedAt    string
		EndedAt      string
		OpeningFloat int64
	}

	activeRows, _ := h.db.QueryContext(r.Context(), `
		SELECT s.id, w.name, TO_CHAR(s.started_at, 'HH24:MI'), s.opening_float
		FROM sessions s JOIN workers w ON s.worker_id = w.id
		WHERE s.shop_id = $1 AND s.status = 'active' ORDER BY s.started_at DESC
	`, shopID)
	defer activeRows.Close()

	var activeSessions []session
	for activeRows.Next() {
		var s session
		activeRows.Scan(&s.ID, &s.Worker, &s.StartedAt, &s.OpeningFloat)
		activeSessions = append(activeSessions, s)
	}

	closedRows, _ := h.db.QueryContext(r.Context(), `
		SELECT s.id, w.name, TO_CHAR(s.started_at, 'HH24:MI'), TO_CHAR(s.ended_at, 'HH24:MI')
		FROM sessions s JOIN workers w ON s.worker_id = w.id
		WHERE s.shop_id = $1 AND s.status = 'closed' ORDER BY s.started_at DESC LIMIT 10
	`, shopID)
	defer closedRows.Close()

	var closedSessions []session
	for closedRows.Next() {
		var s session
		closedRows.Scan(&s.ID, &s.Worker, &s.StartedAt, &s.EndedAt)
		closedSessions = append(closedSessions, s)
	}

	type worker struct {
		ID   string
		Name string
	}

	workerRows, _ := h.db.QueryContext(r.Context(), `
		SELECT id, name FROM workers WHERE shop_id = $1 AND active = true ORDER BY name
	`, shopID)
	defer workerRows.Close()

	var workers []worker
	for workerRows.Next() {
		var wk worker
		workerRows.Scan(&wk.ID, &wk.Name)
		workers = append(workers, wk)
	}

	h.templates["sessions"].Execute(w, struct {
		pageData
		ActiveSessions []session
		ClosedSessions []session
		Workers        []worker
	}{
		pageData:       userCtx,
		ActiveSessions: activeSessions,
		ClosedSessions: closedSessions,
		Workers:        workers,
	})
}

func (h *Handler) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	workerID := r.FormValue("worker_id")
	openingFloat := r.FormValue("opening_float")
	if openingFloat == "" {
		openingFloat = "0"
	}

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO sessions (shop_id, worker_id, opening_float) VALUES ($1, $2, $3)
	`, shopID, workerID, openingFloat)

	if err != nil {
		slog.Error("open session failed", "error", err)
	}

	http.Redirect(w, r, "/sessions", http.StatusSeeOther)
}

func (h *Handler) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	_, err := h.db.ExecContext(r.Context(), `
		UPDATE sessions SET status = 'closed', ended_at = NOW() WHERE id = $1
	`, sessionID)

	if err != nil {
		slog.Error("close session failed", "error", err)
	}

	http.Redirect(w, r, "/sessions", http.StatusSeeOther)
}

func (h *Handler) handleReports(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "reports", "Reports")

	var totalSales, cashTotal, mpesaTotal int64
	var saleCount int

	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0), COUNT(*)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&totalSales, &saleCount)

	_ = h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND (event_data->>'payment')::int = 1 AND created_at >= CURRENT_DATE
	`, shopID).Scan(&cashTotal)

	mpesaTotal = totalSales - cashTotal

	type workerStat struct {
		Worker string
		Sales  int
		Total  int64
	}

	wsRows, _ := h.db.QueryContext(r.Context(), `
		SELECT event_data->>'worker_id', COUNT(*), SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
		GROUP BY event_data->>'worker_id' ORDER BY SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint) DESC
	`, shopID)
	defer wsRows.Close()

	var workerStats []workerStat
	for wsRows.Next() {
		var ws workerStat
		wsRows.Scan(&ws.Worker, &ws.Sales, &ws.Total)
		workerStats = append(workerStats, ws)
	}

	type recentSale struct {
		Time      string
		ProductID string
		Quantity  int64
		Price     int64
		Total     int64
		WorkerID  string
		Payment   int
	}

	rsRows, _ := h.db.QueryContext(r.Context(), `
		SELECT TO_CHAR(created_at, 'HH24:MI'), event_data->>'product_id',
			(event_data->>'quantity')::bigint, (event_data->>'price')::bigint,
			(event_data->>'price')::bigint * (event_data->>'quantity')::bigint,
			event_data->>'worker_id', (event_data->>'payment')::int
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		ORDER BY created_at DESC LIMIT 20
	`, shopID)
	defer rsRows.Close()

	var recentSales []recentSale
	for rsRows.Next() {
		var rs recentSale
		rsRows.Scan(&rs.Time, &rs.ProductID, &rs.Quantity, &rs.Price, &rs.Total, &rs.WorkerID, &rs.Payment)
		recentSales = append(recentSales, rs)
	}

	var shopName string
	h.db.QueryRowContext(r.Context(), `SELECT name FROM shops WHERE id = $1`, shopID).Scan(&shopName)

	h.templates["reports"].Execute(w, struct {
		pageData
		ShopName    string
		TotalSales  string
		SaleCount   int
		CashTotal   string
		MpesaTotal  string
		WorkerStats []workerStat
		RecentSales []recentSale
	}{
		pageData:    userCtx,
		ShopName:    shopName,
		TotalSales:  fmt.Sprintf("%d", totalSales),
		SaleCount:   saleCount,
		CashTotal:   fmt.Sprintf("%d", cashTotal),
		MpesaTotal:  fmt.Sprintf("%d", mpesaTotal),
		WorkerStats: workerStats,
		RecentSales: recentSales,
	})
}

func (h *Handler) handleForecast(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "forecast", "Forecast")

	// Today's revenue
	var todayRevenue int64
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todayRevenue)

	// Revenue forecast
	var last7, last30, next7 float64
	var growthRate float64
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= NOW() - INTERVAL '7 days'
	`, shopID).Scan(&last7)
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= NOW() - INTERVAL '30 days'
	`, shopID).Scan(&last30)

	if last7 > 0 {
		prev7 := last30 - last7
		if prev7 > 0 {
			growthRate = ((last7 - prev7) / prev7) * 100
		}
	}
	next7 = last7 * (1 + growthRate/100*0.5)

	// Inventory forecasts
	type invForecast struct {
		ProductID        string
		CurrentStock     int64
		DailyAvgSales    float64
		DaysUntilStock   int
		ReorderPoint     int64
		RecommendedOrder int64
		Confidence       float64
	}
	var invForecasts []invForecast

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT id, name, stock_qty, min_stock FROM products WHERE shop_id = $1 AND active = true
	`, shopID)
	defer rows.Close()

	for rows.Next() {
		var id, name string
		var stock, minStock int64
		rows.Scan(&id, &name, &stock, &minStock)

		var totalQty, totalDays int64
		h.db.QueryRowContext(r.Context(), `
			SELECT COALESCE(SUM((event_data->>'quantity')::bigint), 0),
				COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(created_at))) / 86400, 1)
			FROM events WHERE shop_id = $1 AND event_type = 'sale' AND event_data->>'product_id' = $2
		`, shopID, id).Scan(&totalQty, &totalDays)

		dailyAvg := float64(totalQty) / float64(totalDays)
		if dailyAvg < 0 {
			dailyAvg = 0
		}

		daysLeft := 999
		if dailyAvg > 0 {
			daysLeft = int(float64(stock) / dailyAvg)
		}

		reorderPoint := int64(dailyAvg * 7)
		recOrder := int64(dailyAvg * 14)
		confidence := 50.0
		if totalDays >= 7 {
			confidence = 70.0
		}
		if totalDays >= 30 {
			confidence = 90.0
		}

		invForecasts = append(invForecasts, invForecast{
			ProductID:        name,
			CurrentStock:     stock,
			DailyAvgSales:    dailyAvg,
			DaysUntilStock:   daysLeft,
			ReorderPoint:     reorderPoint,
			RecommendedOrder: recOrder,
			Confidence:       confidence,
		})
	}

	var todayCash, todayMpesa int64
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 1 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 2 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todayCash, &todayMpesa)

	// Stock health counts
	var healthyStock, criticalStock int64
	h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty > min_stock AND active = true
	`, shopID).Scan(&healthyStock)
	h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty = 0 AND active = true
	`, shopID).Scan(&criticalStock)

	type alertItem struct {
		ProductID      string
		CurrentStock   int64
		ReorderPoint   int64
		DaysUntilStock int
		Urgency        string
	}
	var alerts []alertItem

	alertRows, _ := h.db.QueryContext(r.Context(), `
		SELECT name, stock_qty, min_stock FROM products WHERE shop_id = $1 AND stock_qty <= min_stock AND active = true ORDER BY stock_qty ASC
	`, shopID)
	defer alertRows.Close()

	for alertRows.Next() {
		var name string
		var stock, minStock int64
		alertRows.Scan(&name, &stock, &minStock)

		var totalQty, totalDays int64
		h.db.QueryRowContext(r.Context(), `
			SELECT COALESCE(SUM((event_data->>'quantity')::bigint), 0),
				COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(created_at))) / 86400, 1)
			FROM events WHERE shop_id = $1 AND event_type = 'sale' AND event_data->>'product_id' = (SELECT id FROM products WHERE name = $2 AND shop_id = $1 LIMIT 1)
		`, shopID, name).Scan(&totalQty, &totalDays)

		dailyAvg := float64(totalQty) / float64(totalDays)
		daysLeft := 999
		if dailyAvg > 0 {
			daysLeft = int(float64(stock) / dailyAvg)
		}

		urgency := "low"
		if daysLeft <= 2 {
			urgency = "critical"
		} else if daysLeft <= 5 {
			urgency = "high"
		} else if daysLeft <= 10 {
			urgency = "medium"
		}

		alerts = append(alerts, alertItem{
			ProductID:      name,
			CurrentStock:   stock,
			ReorderPoint:   minStock,
			DaysUntilStock: daysLeft,
			Urgency:        urgency,
		})
	}

	// Demand trends
	type trendItem struct {
		ProductID  string
		ChangePct  float64
		Trend      string
		Prediction string
	}
	var trends []trendItem

	trendRows, _ := h.db.QueryContext(r.Context(), `
		SELECT p.name,
			COALESCE(SUM(CASE WHEN e.created_at >= NOW() - INTERVAL '7 days' THEN (e.event_data->>'quantity')::bigint ELSE 0 END), 0) as recent,
			COALESCE(SUM(CASE WHEN e.created_at >= NOW() - INTERVAL '14 days' AND e.created_at < NOW() - INTERVAL '7 days' THEN (e.event_data->>'quantity')::bigint ELSE 0 END), 0) as prev
		FROM events e JOIN products p ON (e.event_data->>'product_id')::uuid = p.id
		WHERE e.shop_id = $1 AND e.event_type = 'sale'
		GROUP BY p.name ORDER BY recent DESC LIMIT 5
	`, shopID)
	defer trendRows.Close()

	for trendRows.Next() {
		var name string
		var recent, prev int64
		trendRows.Scan(&name, &recent, &prev)

		changePct := 0.0
		if prev > 0 {
			changePct = float64(recent-prev) / float64(prev) * 100
		}

		trend := "stable"
		prediction := "Demand expected to remain steady"
		if changePct > 10 {
			trend = "rising"
			prediction = "Demand increasing — consider ordering more"
		} else if changePct < -10 {
			trend = "falling"
			prediction = "Demand decreasing — hold off on orders"
		}

		trends = append(trends, trendItem{
			ProductID:  name,
			ChangePct:  changePct,
			Trend:      trend,
			Prediction: prediction,
		})
	}

	data := struct {
		pageData
		TodayRevenue       int64
		TodayCash          int64
		TodayMpesa         int64
		Next7Days          float64
		Last7Days          float64
		Last30Days         float64
		GrowthRate         float64
		InventoryForecasts []invForecast
		LowStockAlerts     int
		LowStockAlertsList []alertItem
		DemandTrends       []trendItem
		HealthyStock       int64
		CriticalStock      int64
	}{
		pageData:           userCtx,
		TodayRevenue:       todayRevenue,
		TodayCash:          todayCash,
		TodayMpesa:         todayMpesa,
		Next7Days:          next7,
		Last7Days:          last7,
		Last30Days:         last30,
		GrowthRate:         growthRate,
		InventoryForecasts: invForecasts,
		LowStockAlerts:     len(alerts),
		LowStockAlertsList: alerts,
		DemandTrends:       trends,
		HealthyStock:       healthyStock,
		CriticalStock:      criticalStock,
	}

	if err := h.templates["forecast"].Execute(w, data); err != nil {
		slog.Error("forecast template failed", "error", err)
		http.Error(w, "Template error: "+err.Error(), 500)
	}
}

func (h *Handler) requireOwner(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read role from context (set by requireAuth)
		role, _ := r.Context().Value("user_role").(string)
		if role == "" {
			// Fallback: read from cookie
			if c, _ := r.Cookie("user_role"); c != nil {
				role = c.Value
			}
		}
		if role != "owner" {
			slog.Info("requireOwner: forbidden", "role", role)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}
}

type userInfo struct {
	ID     string
	Name   string
	Email  string
	Role   string
	Active bool
}

func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "users", "User Management")

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT id, name, email, role, active FROM shop_users WHERE shop_id = $1 ORDER BY role, name
	`, shopID)
	defer rows.Close()

	var users []userInfo
	for rows.Next() {
		var u userInfo
		rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.Active)
		users = append(users, u)
	}

	tmpl := h.templates["users"]
	if tmpl == nil {
		slog.Error("users template not loaded")
		http.Error(w, "Template not loaded", 500)
		return
	}
	if err := tmpl.Execute(w, struct {
		pageData
		Users   []userInfo
		Success string
		Error   string
	}{
		pageData: userCtx,
		Users:    users,
		Success:  r.URL.Query().Get("success"),
		Error:    r.URL.Query().Get("error"),
	}); err != nil {
		slog.Error("users template error", "error", err)
		http.Error(w, err.Error(), 500)
	}
}

func (h *Handler) handleAddUser(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	name := r.FormValue("name")
	email := r.FormValue("email")
	role := r.FormValue("role")
	password := r.FormValue("password")

	if role != "cashier" && role != "manager" {
		role = "cashier"
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+create+user", http.StatusSeeOther)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `
		INSERT INTO shop_users (shop_id, name, email, password_hash, role) VALUES ($1, $2, $3, $4, $5)
	`, shopID, name, email, hash, role)

	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Email+already+exists", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/users?success=User+added", http.StatusSeeOther)
}

func (h *Handler) handleToggleUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	_, _ = h.db.ExecContext(r.Context(), `UPDATE shop_users SET active = NOT active WHERE id = $1`, userID)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	role := r.FormValue("role")
	if role != "cashier" && role != "manager" {
		role = "cashier"
	}
	_, _ = h.db.ExecContext(r.Context(), `UPDATE shop_users SET role = $1 WHERE id = $2`, role, userID)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleMpesaCallback(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	result, err := mpesa.ParseCallback(raw)
	if err != nil {
		slog.Error("mpesa callback parse error", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !result.Success {
		slog.Info("mpesa payment failed",
			"checkout_request_id", result.ResultDesc,
			"result_code", result.ResultCode,
		)
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Info("mpesa payment confirmed",
		"receipt", result.MpesaReceipt,
		"amount", result.Amount,
		"phone", result.PhoneNumber,
	)

	w.WriteHeader(http.StatusOK)
}

// ==================== Settings ====================

type settingsData struct {
	pageData
	ShopName       string
	Currency       string
	TaxRate        string
	ShopAddress    string
	ShopPhone      string
	MpesaKey       string
	MpesaSecret    string
	MpesaShortcode string
	MpesaPasskey   string
	SMTPHost       string
	SMTPPort       string
	SMTPUser       string
	SMTPPass       string
	ReportEmail    string
	DailyReport    bool
	Success        string
	Error          string
}

func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r, "settings", "Settings")

	var shopName, currency, address, phone string
	var taxRate float64
	_ = h.db.QueryRowContext(r.Context(), `
		SELECT name, currency, tax_rate, COALESCE(address,''), COALESCE(phone,'') FROM shops WHERE id = $1
	`, shopID).Scan(&shopName, &currency, &taxRate, &address, &phone)

	data := settingsData{
		pageData:    userCtx,
		ShopName:    shopName,
		Currency:    currency,
		TaxRate:     fmt.Sprintf("%.2f", taxRate),
		ShopAddress: address,
		ShopPhone:   phone,
	}

	h.templates["settings"].Execute(w, data)
}

func (h *Handler) handleSettingsShop(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	shopName := r.FormValue("shop_name")
	currency := r.FormValue("currency")
	taxRate := r.FormValue("tax_rate")
	address := r.FormValue("shop_address")
	phone := r.FormValue("shop_phone")

	_, err := h.db.ExecContext(r.Context(), `
		UPDATE shops SET name = $1, currency = $2, tax_rate = $3, address = $4, phone = $5 WHERE id = $6
	`, shopName, currency, taxRate, address, phone, shopID)

	if err != nil {
		http.Redirect(w, r, "/settings?error=Failed+to+save", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings?success=Shop+settings+saved", http.StatusSeeOther)
}

func (h *Handler) handleSettingsMpesa(w http.ResponseWriter, r *http.Request) {
	// Store in system_metrics for now
	shopID := getContextValue(r, "shop_id")

	key := r.FormValue("mpesa_key")
	secret := r.FormValue("mpesa_secret")
	shortcode := r.FormValue("mpesa_shortcode")
	passkey := r.FormValue("mpesa_passkey")

	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'mpesa_key', $2) ON CONFLICT DO NOTHING`, shopID, key)
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'mpesa_secret', $2) ON CONFLICT DO NOTHING`, shopID, secret)
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'mpesa_shortcode', $2) ON CONFLICT DO NOTHING`, shopID, shortcode)
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'mpesa_passkey', $2) ON CONFLICT DO NOTHING`, shopID, passkey)

	http.Redirect(w, r, "/settings?success=M-Pesa+settings+saved", http.StatusSeeOther)
}

func (h *Handler) handleSettingsEmail(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'smtp_host', $2) ON CONFLICT DO NOTHING`, shopID, r.FormValue("smtp_host"))
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'smtp_port', $2) ON CONFLICT DO NOTHING`, shopID, r.FormValue("smtp_port"))
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'smtp_user', $2) ON CONFLICT DO NOTHING`, shopID, r.FormValue("smtp_user"))
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'smtp_pass', $2) ON CONFLICT DO NOTHING`, shopID, r.FormValue("smtp_pass"))
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'report_email', $2) ON CONFLICT DO NOTHING`, shopID, r.FormValue("report_email"))
	h.db.ExecContext(r.Context(), `INSERT INTO system_metrics (shop_id, metric_name, metric_value) VALUES ($1, 'daily_report', $2) ON CONFLICT DO NOTHING`, shopID, r.FormValue("daily_report"))

	http.Redirect(w, r, "/settings?success=Email+settings+saved", http.StatusSeeOther)
}

// ==================== CSV Export ====================

func (h *Handler) handleExportSales(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT event_seq, event_data->>'product_id', (event_data->>'quantity')::bigint,
			(event_data->>'price')::bigint, (event_data->>'worker_id'),
			(event_data->>'payment')::int, created_at
		FROM events WHERE shop_id = $1 AND event_type = 'sale' ORDER BY event_seq
	`, shopID)
	if err != nil {
		http.Error(w, "Query failed", 500)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=sales.csv")
	w.Write([]byte("Seq,Product,Quantity,Price,Worker,Payment,Total,Date\n"))

	for rows.Next() {
		var seq int64
		var product string
		var qty, price int64
		var worker string
		var payment int
		var date time.Time
		rows.Scan(&seq, &product, &qty, &price, &worker, &payment, &date)
		paymentStr := "Cash"
		if payment == 2 {
			paymentStr = "M-Pesa"
		}
		fmt.Fprintf(w, "%d,%s,%d,%d,%s,%s,%d,%s\n",
			seq, product, qty, price, worker, paymentStr, price*qty, date.Format("2006-01-02 15:04"))
	}
}

func (h *Handler) handleExportInventory(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT name, COALESCE(sku,''), COALESCE(category,''), cost_price, sell_price, stock_qty, min_stock, active
		FROM products WHERE shop_id = $1 ORDER BY name
	`, shopID)
	if err != nil {
		http.Error(w, "Query failed", 500)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=inventory.csv")
	w.Write([]byte("Name,SKU,Category,Cost,Sell,Stock,MinStock,Active\n"))

	for rows.Next() {
		var name, sku, cat string
		var cost, sell, stock, minStock int64
		var active bool
		rows.Scan(&name, &sku, &cat, &cost, &sell, &stock, &minStock, &active)
		fmt.Fprintf(w, "%s,%s,%s,%d,%d,%d,%d,%t\n", name, sku, cat, cost, sell, stock, minStock, active)
	}
}

func (h *Handler) handleExportWorkers(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT name, pin IS NOT NULL, active, created_at FROM workers WHERE shop_id = $1 ORDER BY name
	`, shopID)
	if err != nil {
		http.Error(w, "Query failed", 500)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=workers.csv")
	w.Write([]byte("Name,HasPIN,Active,Created\n"))

	for rows.Next() {
		var name string
		var hasPIN, active bool
		var created time.Time
		rows.Scan(&name, &hasPIN, &active, &created)
		fmt.Fprintf(w, "%s,%t,%t,%s\n", name, hasPIN, active, created.Format("2006-01-02"))
	}
}

// ==================== Receipt ====================

func (h *Handler) handleReceipt(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	shopID := getContextValue(r, "shop_id")

	var eventData string
	var createdAt time.Time
	err := h.db.QueryRowContext(r.Context(), `
		SELECT event_data, created_at FROM events WHERE id = $1 AND shop_id = $2 AND event_type = 'sale'
	`, eventID, shopID).Scan(&eventData, &createdAt)
	if err != nil {
		http.Error(w, "Sale not found", 404)
		return
	}

	var sale struct {
		ProductID string `json:"product_id"`
		Quantity  int64  `json:"quantity"`
		Price     int64  `json:"price"`
		WorkerID  string `json:"worker_id"`
		Payment   int    `json:"payment"`
		Total     int64  `json:"total"`
	}
	json.Unmarshal([]byte(eventData), &sale)

	var shopName, currency string
	_ = h.db.QueryRowContext(r.Context(), `SELECT name, currency FROM shops WHERE id = $1`, shopID).Scan(&shopName, &currency)

	paymentStr := "Cash"
	if sale.Payment == 2 {
		paymentStr = "M-Pesa"
	}

	pdfBytes, err := receipt.GeneratePDF(receipt.Receipt{
		ShopName:  shopName,
		ReceiptNo: fmt.Sprintf("RCP-%s", eventID),
		Date:      createdAt,
		Items: []receipt.ReceiptItem{
			{Name: sale.ProductID, Quantity: sale.Quantity, Price: sale.Price, Total: sale.Total},
		},
		Subtotal: sale.Total,
		Total:    sale.Total,
		Payment:  paymentStr,
		Worker:   sale.WorkerID,
		Currency: currency,
	})
	if err != nil {
		http.Error(w, "Failed to generate receipt", 500)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=receipt-%s.pdf", eventID))
	w.Write(pdfBytes)
}

// ==================== Chart Data APIs ====================

func (h *Handler) handleChartSalesTrend(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT DATE(created_at) as sale_date,
			COUNT(*) as count,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0) as total
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY DATE(created_at) ORDER BY sale_date
	`, shopID)
	defer rows.Close()

	type dataPoint struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
		Total int64  `json:"total"`
	}
	var points []dataPoint
	for rows.Next() {
		var dp dataPoint
		rows.Scan(&dp.Date, &dp.Count, &dp.Total)
		points = append(points, dp)
	}
	if points == nil {
		points = []dataPoint{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": points})
}

func (h *Handler) handleChartPaymentMethods(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT (event_data->>'payment')::int as payment,
			COUNT(*) as count,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0) as total
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		GROUP BY (event_data->>'payment')::int
	`, shopID)
	defer rows.Close()

	type paymentData struct {
		Method string `json:"method"`
		Count  int    `json:"count"`
		Total  int64  `json:"total"`
	}
	var data []paymentData
	for rows.Next() {
		var pd paymentData
		rows.Scan(&pd.Method, &pd.Count, &pd.Total)
		if pd.Method == "1" {
			pd.Method = "Cash"
		} else {
			pd.Method = "M-Pesa"
		}
		data = append(data, pd)
	}
	if data == nil {
		data = []paymentData{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
}

func (h *Handler) handleChartTopProducts(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT p.name,
			SUM((e.event_data->>'quantity')::bigint) as qty,
			COALESCE(SUM((e.event_data->>'price')::bigint * (e.event_data->>'quantity')::bigint), 0) as revenue
		FROM events e
		JOIN products p ON (e.event_data->>'product_id')::uuid = p.id
		WHERE e.shop_id = $1 AND e.event_type = 'sale'
		GROUP BY p.name ORDER BY revenue DESC LIMIT 8
	`, shopID)
	defer rows.Close()

	type productData struct {
		Name    string `json:"name"`
		Qty     int64  `json:"qty"`
		Revenue int64  `json:"revenue"`
	}
	var data []productData
	for rows.Next() {
		var pd productData
		rows.Scan(&pd.Name, &pd.Qty, &pd.Revenue)
		data = append(data, pd)
	}
	if data == nil {
		data = []productData{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
}

// ==================== Offline Sync ====================

func (h *Handler) handleSyncOfflineSales(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userID := getContextValue(r, "user_id")

	var req struct {
		Sales []struct {
			ID    string `json:"id"`
			Items []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Price int64  `json:"price"`
				Qty   int64  `json:"qty"`
			} `json:"items"`
			Payment  string `json:"payment"`
			WorkerID string `json:"worker_id"`
			Time     string `json:"timestamp"`
		} `json:"sales"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	synced := []string{}
	failed := []string{}

	for _, sale := range req.Sales {
		err := h.db.Transactional(r.Context(), func(tx *sql.Tx) error {
			for _, item := range sale.Items {
				var currentStock int64
				err := tx.QueryRowContext(r.Context(),
					`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2 AND active = true`,
					item.ID, shopID,
				).Scan(&currentStock)
				if err != nil {
					return fmt.Errorf("product %s not found", item.Name)
				}

				_, err = tx.ExecContext(r.Context(),
					`UPDATE products SET stock_qty = stock_qty - $1 WHERE id = $2 AND shop_id = $3`,
					item.Qty, item.ID, shopID,
				)
				if err != nil {
					return err
				}

				eventData, _ := json.Marshal(map[string]interface{}{
					"product_id": item.ID,
					"quantity":   item.Qty,
					"price":      item.Price,
					"worker_id":  sale.WorkerID,
					"payment":    sale.Payment,
					"total":      item.Price * item.Qty,
				})

				var lastHash string
				_ = tx.QueryRowContext(r.Context(),
					`SELECT COALESCE(event_hash, '') FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`,
					shopID,
				).Scan(&lastHash)

				var seq int64
				_ = tx.QueryRowContext(r.Context(),
					`SELECT COALESCE(MAX(event_seq), 0) FROM events WHERE shop_id = $1`,
					shopID,
				).Scan(&seq)
				seq++

				content := string(eventData) + lastHash
				sum := sha256.Sum256([]byte(content))
				eventHash := hex.EncodeToString(sum[:])

				_, err = tx.ExecContext(r.Context(), `
					INSERT INTO events (shop_id, event_seq, event_type, event_data, previous_hash, event_hash, created_by)
					VALUES ($1, $2, 'sale', $3, $4, $5, $6)
				`, shopID, seq, string(eventData), lastHash, eventHash, userID)
				if err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			slog.Error("sync failed for sale", "id", sale.ID, "error", err)
			failed = append(failed, sale.ID)
		} else {
			synced = append(synced, sale.ID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"synced": synced,
		"failed": failed,
	})
}

// ==================== Forecast Chart APIs ====================

func (h *Handler) handleForecastRevenueTrend(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	type dayData struct {
		Date  string `json:"date"`
		Total int64  `json:"total"`
	}

	// Last 30 days of actual revenue
	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT DATE(created_at) as d,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY DATE(created_at) ORDER BY d
	`, shopID)
	defer rows.Close()

	history := make(map[string]int64)
	for rows.Next() {
		var d string
		var t int64
		rows.Scan(&d, &t)
		history[d] = t
	}

	var last7, last30, todayRevenue float64
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= NOW() - INTERVAL '7 days'
	`, shopID).Scan(&last7)
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= NOW() - INTERVAL '30 days'
	`, shopID).Scan(&last30)
	h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todayRevenue)

	growthRate := 0.0
	if last7 > 0 {
		prev7 := last30 - last7
		if prev7 > 0 {
			growthRate = ((last7 - prev7) / prev7) * 100
		}
	}
	dailyAvg := last7 / 7.0
	projected := dailyAvg * (1 + growthRate/100*0.5)

	// Build 30-day history + 7-day projection
	var labels []string
	var actual []interface{}
	var projectedData []interface{}

	for i := 29; i >= 0; i-- {
		d := time.Now().AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		label := d.Format("Jan 2")
		if i == 0 {
			label = "Today"
		} else if i == 29 {
			label = "30d ago"
		}
		labels = append(labels, label)
		if v, ok := history[dateStr]; ok {
			actual = append(actual, v)
		} else {
			actual = append(actual, 0)
		}
		projectedData = append(projectedData, nil)
	}

	// 7-day projection
	for i := 1; i <= 7; i++ {
		labels = append(labels, fmt.Sprintf("+%dd", i))
		actual = append(actual, nil)
		if i == 1 {
			projectedData = append(projectedData, int64(todayRevenue))
		} else {
			projectedData = append(projectedData, int64(projected))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"labels":    labels,
		"actual":    actual,
		"projected": projectedData,
	})
}

func (h *Handler) handleForecastProductRadar(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT p.name,
			COALESCE(SUM((e.event_data->>'price')::bigint * (e.event_data->>'quantity')::bigint), 0) as revenue
		FROM events e
		JOIN products p ON (e.event_data->>'product_id')::uuid = p.id
		WHERE e.shop_id = $1 AND e.event_type = 'sale'
		GROUP BY p.name ORDER BY revenue DESC LIMIT 5
	`, shopID)
	defer rows.Close()

	var labels []string
	var data []int64
	for rows.Next() {
		var name string
		var rev int64
		rows.Scan(&name, &rev)
		labels = append(labels, name)
		data = append(data, rev)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"labels": labels,
		"data":   data,
	})
}

func (h *Handler) handleForecastDailyPattern(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT EXTRACT(DOW FROM created_at)::int as dow,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY EXTRACT(DOW FROM created_at) ORDER BY dow
	`, shopID)
	defer rows.Close()

	dayMap := make(map[int]int64)
	for rows.Next() {
		var dow int
		var total int64
		rows.Scan(&dow, &total)
		dayMap[dow] = total
	}

	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var data []int64
	for i := 0; i < 7; i++ {
		data = append(data, dayMap[i])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"labels": days,
		"data":   data,
	})
}

func (h *Handler) handleForecastStockHealth(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")

	var healthy, low, critical int64
	h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty > min_stock AND active = true
	`, shopID).Scan(&healthy)
	h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty > 0 AND stock_qty <= min_stock AND active = true
	`, shopID).Scan(&low)
	h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty = 0 AND active = true
	`, shopID).Scan(&critical)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"healthy":  healthy,
		"low":      low,
		"critical": critical,
	})
}

// ==================== Report APIs ====================

func dateRangeFilter(r *http.Request) string {
	rng := r.URL.Query().Get("range")
	switch rng {
	case "today":
		return "AND created_at >= CURRENT_DATE"
	case "7d":
		return "AND created_at >= NOW() - INTERVAL '7 days'"
	case "30d":
		return "AND created_at >= NOW() - INTERVAL '30 days'"
	default:
		return ""
	}
}

func (h *Handler) handleReportSummary(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	filter := dateRangeFilter(r)

	var total, count, cash, mpesa int64
	h.db.QueryRowContext(r.Context(), fmt.Sprintf(`
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0),
			COUNT(*),
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 1 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 2 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' %s
	`, filter), shopID).Scan(&total, &count, &cash, &mpesa)

	// Revenue over time
	var labels []string
	var data []int64
	rows, _ := h.db.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT DATE(created_at) as d,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' %s
		GROUP BY DATE(created_at) ORDER BY d
	`, filter), shopID)
	defer rows.Close()
	for rows.Next() {
		var d string
		var t int64
		rows.Scan(&d, &t)
		labels = append(labels, d)
		data = append(data, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":         total,
		"count":         count,
		"cash":          cash,
		"mpesa":         mpesa,
		"revenueLabels": labels,
		"revenueData":   data,
	})
}

func (h *Handler) handleReportTransactions(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	filter := dateRangeFilter(r)

	rows, _ := h.db.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT id, event_data, TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI')
		FROM events WHERE shop_id = $1 AND event_type = 'sale' %s
		ORDER BY created_at DESC LIMIT 200
	`, filter), shopID)
	defer rows.Close()

	type tx struct {
		ID      string `json:"id"`
		Time    string `json:"time"`
		Product string `json:"product"`
		Qty     int64  `json:"qty"`
		Price   int64  `json:"price"`
		Total   int64  `json:"total"`
		Worker  string `json:"worker"`
		Payment string `json:"payment"`
	}
	var txns []tx
	for rows.Next() {
		var id, dataStr, timeStr string
		rows.Scan(&id, &dataStr, &timeStr)
		var sale struct {
			ProductID string `json:"product_id"`
			Quantity  int64  `json:"quantity"`
			Price     int64  `json:"price"`
			WorkerID  string `json:"worker_id"`
			Payment   int    `json:"payment"`
		}
		json.Unmarshal([]byte(dataStr), &sale)
		payment := "Cash"
		if sale.Payment == 2 {
			payment = "M-Pesa"
		}
		txns = append(txns, tx{
			ID: id, Time: timeStr, Product: sale.ProductID,
			Qty: sale.Quantity, Price: sale.Price, Total: sale.Price * sale.Quantity,
			Worker: sale.WorkerID, Payment: payment,
		})
	}
	if txns == nil {
		txns = []tx{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txns)
}

func (h *Handler) handleReportWorkers(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	filter := dateRangeFilter(r)

	rows, _ := h.db.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT event_data->>'worker_id' as worker,
			COUNT(*) as count,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0) as total,
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 1 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0) as cash,
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 2 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0) as mpesa
		FROM events WHERE shop_id = $1 AND event_type = 'sale' %s
		GROUP BY event_data->>'worker_id' ORDER BY total DESC
	`, filter), shopID)
	defer rows.Close()

	type wk struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
		Total int64  `json:"total"`
		Cash  int64  `json:"cash"`
		Mpesa int64  `json:"mpesa"`
	}
	var workers []wk
	for rows.Next() {
		var w wk
		rows.Scan(&w.Name, &w.Count, &w.Total, &w.Cash, &w.Mpesa)
		workers = append(workers, w)
	}
	if workers == nil {
		workers = []wk{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(workers)
}

func (h *Handler) handleReportProducts(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	filter := dateRangeFilter(r)

	rows, _ := h.db.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT event_data->>'product_id' as product,
			COALESCE(SUM((event_data->>'quantity')::bigint), 0) as qty,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0) as revenue
		FROM events WHERE shop_id = $1 AND event_type = 'sale' %s
		GROUP BY event_data->>'product_id' ORDER BY revenue DESC
	`, filter), shopID)
	defer rows.Close()

	type prod struct {
		Name    string `json:"name"`
		Qty     int64  `json:"qty"`
		Revenue int64  `json:"revenue"`
	}
	var products []prod
	for rows.Next() {
		var p prod
		rows.Scan(&p.Name, &p.Qty, &p.Revenue)
		products = append(products, p)
	}
	if products == nil {
		products = []prod{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

func (h *Handler) handleReportHourly(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	filter := dateRangeFilter(r)

	rows, _ := h.db.QueryContext(r.Context(), fmt.Sprintf(`
		SELECT EXTRACT(HOUR FROM created_at)::int as hour,
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' %s
		GROUP BY EXTRACT(HOUR FROM created_at) ORDER BY hour
	`, filter), shopID)
	defer rows.Close()

	hourMap := make(map[int]int64)
	for rows.Next() {
		var hour int
		var total int64
		rows.Scan(&hour, &total)
		hourMap[hour] = total
	}

	var labels []string
	var data []int64
	for i := 0; i < 24; i++ {
		labels = append(labels, fmt.Sprintf("%02d:00", i))
		data = append(data, hourMap[i])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"labels": labels,
		"data":   data,
	})
}
