package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"mini-database/internal/auth"
	"mini-database/internal/db"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	db        *db.DB
	templates map[string]*template.Template
	authSvc   *auth.Service
}

func NewHandler(database *db.DB, authSvc *auth.Service) *Handler {
	h := &Handler{
		db:        database,
		templates: make(map[string]*template.Template),
		authSvc:   authSvc,
	}
	h.loadTemplates()
	return h
}

func (h *Handler) loadTemplates() {
	pages := []string{
		"login", "signup", "dashboard", "pos", "inventory", "workers", "sessions", "reports",
	}

	for _, page := range pages {
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
	mux.HandleFunc("GET /ghost", h.requireAuth(h.handleGhost))
}

type pageData struct {
	UserName     string
	UserRole     string
	UserInitials string
	Currency     string
}

func (h *Handler) getUserContext(r *http.Request) pageData {
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
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		claims, err := h.authSvc.VerifyToken(cookie.Value)
		if err != nil {
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
		ctx = addContextValue(ctx, "user_name", userName)
		ctx = addContextValue(ctx, "user_role", claims.Role)
		ctx = addContextValue(ctx, "currency", currency)
		ctx = addContextValue(ctx, "shop_id", claims.ShopID)
		ctx = addContextValue(ctx, "user_id", claims.UserID)

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
	userCtx := h.getUserContext(r)

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

	data := struct {
		pageData
		TodaySales     string
		TodayCash      string
		TodayMpesa     string
		TodaySaleCount int
		LowStock       int
		ActiveSessions int
		RiskScore      int
		RecentSales    []recentSale
		Anomalies      []map[string]string
	}{
		pageData:       userCtx,
		TodaySales:     fmt.Sprintf("%d", todaySales),
		TodayCash:      fmt.Sprintf("%d", todayCash),
		TodayMpesa:     fmt.Sprintf("%d", todayMpesa),
		TodaySaleCount: todaySaleCount,
		LowStock:       lowStock,
		ActiveSessions: activeSessions,
		RiskScore:      riskScore,
		RecentSales:    recentSales,
	}

	if err := h.templates["dashboard"].Execute(w, data); err != nil {
		slog.Error("dashboard template failed", "error", err)
		http.Error(w, "Template error: "+err.Error(), 500)
		return
	}
}

func (h *Handler) handlePOS(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r)

	type product struct {
		ID        string
		Name      string
		SellPrice int64
		StockQty  int64
	}

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT id, name, sell_price, stock_qty FROM products WHERE shop_id = $1 AND active = true ORDER BY name
	`, shopID)
	defer rows.Close()

	var products []product
	for rows.Next() {
		var p product
		rows.Scan(&p.ID, &p.Name, &p.SellPrice, &p.StockQty)
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
				"payment":    payment,
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
		slog.Error("checkout failed", "error", err)
		http.Redirect(w, r, "/pos", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/pos?success=Sale+completed", http.StatusSeeOther)
}

func (h *Handler) handleInventory(w http.ResponseWriter, r *http.Request) {
	shopID := getContextValue(r, "shop_id")
	userCtx := h.getUserContext(r)

	type product struct {
		Name      string
		SKU       string
		CostPrice int64
		SellPrice int64
		StockQty  int64
		MinStock  int64
	}

	rows, _ := h.db.QueryContext(r.Context(), `
		SELECT name, COALESCE(sku,''), cost_price, sell_price, stock_qty, min_stock
		FROM products WHERE shop_id = $1 AND active = true ORDER BY name
	`, shopID)
	defer rows.Close()

	var products []product
	for rows.Next() {
		var p product
		rows.Scan(&p.Name, &p.SKU, &p.CostPrice, &p.SellPrice, &p.StockQty, &p.MinStock)
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
	userCtx := h.getUserContext(r)

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
	userCtx := h.getUserContext(r)

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
	userCtx := h.getUserContext(r)

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

	h.templates["reports"].Execute(w, struct {
		pageData
		TotalSales  string
		SaleCount   int
		CashTotal   string
		MpesaTotal  string
		WorkerStats []workerStat
		RecentSales []recentSale
	}{
		pageData:    userCtx,
		TotalSales:  fmt.Sprintf("%d", totalSales),
		SaleCount:   saleCount,
		CashTotal:   fmt.Sprintf("%d", cashTotal),
		MpesaTotal:  fmt.Sprintf("%d", mpesaTotal),
		WorkerStats: workerStats,
		RecentSales: recentSales,
	})
}

func (h *Handler) handleGhost(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
