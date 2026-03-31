package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"mini-database/internal/auth"

	"github.com/go-chi/chi/v5"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	ShopID       string `json:"shop_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
}

func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.db.PingContext(ctx); err != nil {
		respondError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readinessCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.db.PingContext(ctx); err != nil {
		respondError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	var userID, shopID, email, role, passwordHash string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, shop_id, email, role, password_hash FROM shop_users WHERE email = $1 AND active = true`,
		req.Email,
	).Scan(&userID, &shopID, &email, &role, &passwordHash)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		slog.Error("login query failed", "error", err)
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := auth.CheckPassword(passwordHash, req.Password); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	access, refresh, err := s.auth.GenerateToken(userID, shopID, email, role)
	if err != nil {
		slog.Error("token generation failed", "error", err)
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("user logged in", "user_id", userID, "email", email, "role", role)

	respondJSON(w, http.StatusOK, loginResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		UserID:       userID,
		ShopID:       shopID,
		Email:        email,
		Role:         role,
	})
}

func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	claims, err := s.auth.VerifyToken(req.RefreshToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	access, refresh, err := s.auth.GenerateToken(claims.UserID, claims.ShopID, claims.Email, claims.Role)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"access_token":  access,
		"refresh_token": refresh,
	})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var todaySales, todayCash, todayMpesa int64
	var todaySaleCount int
	var lowStockCount int
	var activeSessions int

	_ = s.db.QueryRowContext(r.Context(), `
		SELECT 
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0),
			COUNT(*)
		FROM events 
		WHERE shop_id = $1 AND event_type = 'sale' 
		AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todaySales, &todaySaleCount)

	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events 
		WHERE shop_id = $1 AND event_type = 'sale' 
		AND event_data->>'payment' = '1'
		AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todayCash)

	todayMpesa = todaySales - todayCash

	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty <= min_stock AND active = true
	`, shopID).Scan(&lowStockCount)

	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM sessions WHERE shop_id = $1 AND status = 'active'
	`, shopID).Scan(&activeSessions)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"today_sales":      todaySales,
		"today_sale_count": todaySaleCount,
		"today_cash":       todayCash,
		"today_mpesa":      todayMpesa,
		"low_stock_items":  lowStockCount,
		"active_sessions":  activeSessions,
	})
}

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, sku, category, cost_price, sell_price, stock_qty, min_stock, active, created_at
		FROM products WHERE shop_id = $1 ORDER BY name
	`, shopID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type product struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		SKU       string    `json:"sku"`
		Category  string    `json:"category"`
		CostPrice int64     `json:"cost_price"`
		SellPrice int64     `json:"sell_price"`
		StockQty  int64     `json:"stock_qty"`
		MinStock  int64     `json:"min_stock"`
		Active    bool      `json:"active"`
		CreatedAt time.Time `json:"created_at"`
	}

	var products []product
	for rows.Next() {
		var p product
		if err := rows.Scan(&p.ID, &p.Name, &p.SKU, &p.Category, &p.CostPrice, &p.SellPrice, &p.StockQty, &p.MinStock, &p.Active, &p.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		products = append(products, p)
	}

	if products == nil {
		products = []product{}
	}

	respondJSON(w, http.StatusOK, products)
}

func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var req struct {
		Name      string `json:"name"`
		SKU       string `json:"sku"`
		Category  string `json:"category"`
		CostPrice int64  `json:"cost_price"`
		SellPrice int64  `json:"sell_price"`
		StockQty  int64  `json:"stock_qty"`
		MinStock  int64  `json:"min_stock"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	var id string
	err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO products (shop_id, name, sku, category, cost_price, sell_price, stock_qty, min_stock)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, shopID, req.Name, req.SKU, req.Category, req.CostPrice, req.SellPrice, req.StockQty, req.MinStock).Scan(&id)

	if err != nil {
		slog.Error("create product failed", "error", err)
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("product created", "id", id, "name", req.Name, "shop", shopID)

	respondCreated(w, map[string]string{"id": id, "name": req.Name})
}

func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	productID := chi.URLParam(r, "id")

	var req struct {
		Name      string `json:"name"`
		SKU       string `json:"sku"`
		Category  string `json:"category"`
		CostPrice int64  `json:"cost_price"`
		SellPrice int64  `json:"sell_price"`
		MinStock  int64  `json:"min_stock"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_, err := s.db.ExecContext(r.Context(), `
		UPDATE products SET name = COALESCE(NULLIF($1, ''), name),
			sku = COALESCE(NULLIF($2, ''), sku),
			category = COALESCE(NULLIF($3, ''), category),
			cost_price = $4, sell_price = $5, min_stock = $6,
			updated_at = NOW()
		WHERE id = $7 AND shop_id = $8
	`, req.Name, req.SKU, req.Category, req.CostPrice, req.SellPrice, req.MinStock, productID, shopID)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "update failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) addStock(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	productID := chi.URLParam(r, "id")
	userID := GetUserID(r)

	var req struct {
		Quantity int64 `json:"quantity"`
		Cost     int64 `json:"cost"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Quantity <= 0 {
		respondError(w, http.StatusBadRequest, "quantity must be positive")
		return
	}

	err := s.db.Transactional(r.Context(), func(tx *sql.Tx) error {
		var currentStock int64
		err := tx.QueryRowContext(r.Context(),
			`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2`,
			productID, shopID,
		).Scan(&currentStock)
		if err != nil {
			return fmt.Errorf("product not found: %w", err)
		}

		_, err = tx.ExecContext(r.Context(),
			`UPDATE products SET stock_qty = stock_qty + $1, updated_at = NOW() WHERE id = $2 AND shop_id = $3`,
			req.Quantity, productID, shopID,
		)
		if err != nil {
			return err
		}

		eventData, _ := json.Marshal(map[string]interface{}{
			"product_id": productID,
			"quantity":   req.Quantity,
			"cost":       req.Cost,
			"new_stock":  currentStock + req.Quantity,
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

		eventHash := computeEventHash(eventData, lastHash)

		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO events (shop_id, event_seq, event_type, event_data, previous_hash, event_hash, created_by)
			VALUES ($1, $2, 'stock_in', $3, $4, $5, $6)
		`, shopID, seq, string(eventData), lastHash, eventHash, userID)

		return err
	})

	if err != nil {
		slog.Error("add stock failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to add stock")
		return
	}

	slog.Info("stock added", "product", productID, "qty", req.Quantity, "shop", shopID)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"added":   req.Quantity,
		"product": productID,
	})
}

func (s *Server) recordSale(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	userID := GetUserID(r)

	var req struct {
		ProductID string `json:"product_id"`
		Quantity  int64  `json:"quantity"`
		Price     int64  `json:"price"`
		WorkerID  string `json:"worker_id"`
		Payment   int    `json:"payment"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ProductID == "" || req.Quantity <= 0 || req.Price < 0 || req.WorkerID == "" {
		respondError(w, http.StatusBadRequest, "product_id, quantity (>0), price (>=0), and worker_id are required")
		return
	}

	if req.Payment != 1 && req.Payment != 2 {
		respondError(w, http.StatusBadRequest, "payment must be 1 (cash) or 2 (mpesa)")
		return
	}

	err := s.db.Transactional(r.Context(), func(tx *sql.Tx) error {
		var currentStock int64
		err := tx.QueryRowContext(r.Context(),
			`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2 AND active = true`,
			req.ProductID, shopID,
		).Scan(&currentStock)
		if err != nil {
			return fmt.Errorf("product not found or inactive")
		}

		if currentStock < req.Quantity {
			return fmt.Errorf("insufficient stock: have %d, need %d", currentStock, req.Quantity)
		}

		_, err = tx.ExecContext(r.Context(),
			`UPDATE products SET stock_qty = stock_qty - $1, updated_at = NOW() WHERE id = $2 AND shop_id = $3`,
			req.Quantity, req.ProductID, shopID,
		)
		if err != nil {
			return err
		}

		eventData, _ := json.Marshal(map[string]interface{}{
			"product_id": req.ProductID,
			"quantity":   req.Quantity,
			"price":      req.Price,
			"worker_id":  req.WorkerID,
			"payment":    req.Payment,
			"total":      req.Price * req.Quantity,
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

		eventHash := computeEventHash(eventData, lastHash)

		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO events (shop_id, event_seq, event_type, event_data, previous_hash, event_hash, created_by)
			VALUES ($1, $2, 'sale', $3, $4, $5, $6)
		`, shopID, seq, string(eventData), lastHash, eventHash, userID)

		return err
	})

	if err != nil {
		slog.Error("record sale failed", "error", err)
		if err.Error()[:20] == "insufficient stock" {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to record sale")
		return
	}

	slog.Info("sale recorded", "product", req.ProductID, "qty", req.Quantity, "total", req.Price*req.Quantity, "shop", shopID)

	respondCreated(w, map[string]interface{}{
		"status":   "ok",
		"product":  req.ProductID,
		"quantity": req.Quantity,
		"total":    req.Price * req.Quantity,
	})
}

func (s *Server) listSales(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, event_seq, event_data, created_at
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
		ORDER BY event_seq DESC LIMIT $2
	`, shopID, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type saleRecord struct {
		ID        int64           `json:"id"`
		Seq       int64           `json:"seq"`
		Data      json.RawMessage `json:"data"`
		CreatedAt time.Time       `json:"created_at"`
	}

	var sales []saleRecord
	for rows.Next() {
		var sr saleRecord
		var dataStr string
		if err := rows.Scan(&sr.ID, &sr.Seq, &dataStr, &sr.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		sr.Data = json.RawMessage(dataStr)
		sales = append(sales, sr)
	}

	if sales == nil {
		sales = []saleRecord{}
	}

	respondJSON(w, http.StatusOK, sales)
}

func (s *Server) getReceipt(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "receipt generation coming soon"})
}

func (s *Server) listWorkers(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, pin IS NOT NULL as has_pin, active, created_at
		FROM workers WHERE shop_id = $1 ORDER BY name
	`, shopID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type worker struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		HasPIN    bool      `json:"has_pin"`
		Active    bool      `json:"active"`
		CreatedAt time.Time `json:"created_at"`
	}

	var workers []worker
	for rows.Next() {
		var wk worker
		if err := rows.Scan(&wk.ID, &wk.Name, &wk.HasPIN, &wk.Active, &wk.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		workers = append(workers, wk)
	}

	if workers == nil {
		workers = []worker{}
	}

	respondJSON(w, http.StatusOK, workers)
}

func (s *Server) createWorker(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var req struct {
		Name string `json:"name"`
		PIN  string `json:"pin"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	var id string
	err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO workers (shop_id, name, pin) VALUES ($1, $2, $3) RETURNING id
	`, shopID, req.Name, req.PIN).Scan(&id)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create worker")
		return
	}

	slog.Info("worker created", "id", id, "name", req.Name, "shop", shopID)
	respondCreated(w, map[string]string{"id": id, "name": req.Name})
}

func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var req struct {
		WorkerID     string `json:"worker_id"`
		OpeningFloat int64  `json:"opening_float"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var id string
	err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO sessions (shop_id, worker_id, opening_float) VALUES ($1, $2, $3) RETURNING id
	`, shopID, req.WorkerID, req.OpeningFloat).Scan(&id)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to open session")
		return
	}

	slog.Info("session opened", "id", id, "worker", req.WorkerID, "shop", shopID)
	respondCreated(w, map[string]string{"id": id, "worker_id": req.WorkerID})
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	sessionID := chi.URLParam(r, "id")

	var req struct {
		ClosingCash  int64 `json:"closing_cash"`
		ClosingMpesa int64 `json:"closing_mpesa"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_, err := s.db.ExecContext(r.Context(), `
		UPDATE sessions SET status = 'closed', ended_at = NOW(),
			closing_cash = $1, closing_mpesa = $2
		WHERE id = $3 AND shop_id = $4
	`, req.ClosingCash, req.ClosingMpesa, sessionID, shopID)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to close session")
		return
	}

	slog.Info("session closed", "id", sessionID, "shop", shopID)
	respondJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT s.id, w.name, s.started_at, s.ended_at, s.status, s.opening_float
		FROM sessions s JOIN workers w ON s.worker_id = w.id
		WHERE s.shop_id = $1 ORDER BY s.started_at DESC LIMIT 20
	`, shopID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type session struct {
		ID           string     `json:"id"`
		Worker       string     `json:"worker"`
		StartedAt    time.Time  `json:"started_at"`
		EndedAt      *time.Time `json:"ended_at"`
		Status       string     `json:"status"`
		OpeningFloat int64      `json:"opening_float"`
	}

	var sessions []session
	for rows.Next() {
		var s session
		if err := rows.Scan(&s.ID, &s.Worker, &s.StartedAt, &s.EndedAt, &s.Status, &s.OpeningFloat); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		sessions = append(sessions, s)
	}

	if sessions == nil {
		sessions = []session{}
	}

	respondJSON(w, http.StatusOK, sessions)
}

func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	userID := GetUserID(r)

	var req struct {
		WorkerID      string `json:"worker_id"`
		DeclaredCash  int64  `json:"declared_cash"`
		DeclaredMpesa int64  `json:"declared_mpesa"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var expectedCash, expectedMpesa int64
	err := s.db.QueryRowContext(r.Context(), `
		SELECT 
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 1 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN (event_data->>'payment')::int = 2 THEN (event_data->>'price')::bigint * (event_data->>'quantity')::bigint ELSE 0 END), 0)
		FROM events
		WHERE shop_id = $1 AND event_type = 'sale' AND event_data->>'worker_id' = $2
	`, shopID, req.WorkerID).Scan(&expectedCash, &expectedMpesa)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to calculate expected amounts")
		return
	}

	eventData, _ := json.Marshal(map[string]interface{}{
		"worker_id":      req.WorkerID,
		"expected_cash":  expectedCash,
		"declared_cash":  req.DeclaredCash,
		"expected_mpesa": expectedMpesa,
		"declared_mpesa": req.DeclaredMpesa,
		"cash_variance":  req.DeclaredCash - expectedCash,
		"mpesa_variance": req.DeclaredMpesa - expectedMpesa,
	})

	var lastHash string
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(event_hash, '') FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`,
		shopID,
	).Scan(&lastHash)

	var seq int64
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(event_seq), 0) FROM events WHERE shop_id = $1`,
		shopID,
	).Scan(&seq)
	seq++

	eventHash := computeEventHash(eventData, lastHash)

	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO events (shop_id, event_seq, event_type, event_data, previous_hash, event_hash, created_by)
		VALUES ($1, $2, 'reconciliation', $3, $4, $5, $6)
	`, shopID, seq, string(eventData), lastHash, eventHash, userID)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to record reconciliation")
		return
	}

	respondCreated(w, map[string]interface{}{
		"status":         "ok",
		"expected_cash":  expectedCash,
		"declared_cash":  req.DeclaredCash,
		"cash_variance":  req.DeclaredCash - expectedCash,
		"expected_mpesa": expectedMpesa,
		"declared_mpesa": req.DeclaredMpesa,
		"mpesa_variance": req.DeclaredMpesa - expectedMpesa,
	})
}

func (s *Server) workerReconciliations(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	workerID := chi.URLParam(r, "worker_id")

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT event_data, created_at FROM events
		WHERE shop_id = $1 AND event_type = 'reconciliation' AND event_data->>'worker_id' = $2
		ORDER BY created_at DESC LIMIT 20
	`, shopID, workerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type reconc struct {
		Data      json.RawMessage `json:"data"`
		CreatedAt time.Time       `json:"created_at"`
	}

	var recs []reconc
	for rows.Next() {
		var rec reconc
		var dataStr string
		if err := rows.Scan(&dataStr, &rec.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		rec.Data = json.RawMessage(dataStr)
		recs = append(recs, rec)
	}

	if recs == nil {
		recs = []reconc{}
	}

	respondJSON(w, http.StatusOK, recs)
}

func (s *Server) dailyReport(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var totalSales, totalCash, totalMpesa int64
	var saleCount int

	_ = s.db.QueryRowContext(r.Context(), `
		SELECT 
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0),
			COUNT(*)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&totalSales, &saleCount)

	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND event_data->>'payment' = '1' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&totalCash)

	totalMpesa = totalSales - totalCash

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"date":        time.Now().Format("2006-01-02"),
		"total_sales": totalSales,
		"sale_count":  saleCount,
		"cash":        totalCash,
		"mpesa":       totalMpesa,
	})
}

func (s *Server) workerReport(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT 
			event_data->>'worker_id' as worker,
			COUNT(*) as sales,
			SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint) as total
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
		GROUP BY event_data->>'worker_id'
		ORDER BY total DESC
	`, shopID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type workerStat struct {
		Worker string `json:"worker"`
		Sales  int    `json:"sales"`
		Total  int64  `json:"total"`
	}

	var stats []workerStat
	for rows.Next() {
		var ws workerStat
		if err := rows.Scan(&ws.Worker, &ws.Sales, &ws.Total); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		stats = append(stats, ws)
	}

	if stats == nil {
		stats = []workerStat{}
	}

	respondJSON(w, http.StatusOK, stats)
}

func (s *Server) ledgerReport(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT event_seq, event_type, event_data, event_hash, previous_hash, created_at
		FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT $2
	`, shopID, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type ledgerEntry struct {
		Seq          int64           `json:"seq"`
		Type         string          `json:"type"`
		Data         json.RawMessage `json:"data"`
		Hash         string          `json:"hash"`
		PreviousHash string          `json:"previous_hash"`
		CreatedAt    time.Time       `json:"created_at"`
	}

	var entries []ledgerEntry
	for rows.Next() {
		var e ledgerEntry
		var dataStr string
		if err := rows.Scan(&e.Seq, &e.Type, &dataStr, &e.Hash, &e.PreviousHash, &e.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		e.Data = json.RawMessage(dataStr)
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []ledgerEntry{}
	}

	respondJSON(w, http.StatusOK, entries)
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "csv export coming soon"})
}

func (s *Server) ghostReport(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var anomalies []map[string]interface{}

	rows, err := s.db.QueryContext(r.Context(), `
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
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var worker string
		var total, short int
		var variance int64
		if err := rows.Scan(&worker, &total, &short, &variance); err != nil {
			continue
		}

		shortRate := float64(short) / float64(total) * 100
		if shortRate >= 50 {
			severity := "medium"
			if shortRate >= 80 {
				severity = "high"
			}
			if shortRate >= 95 && total >= 5 {
				severity = "critical"
			}

			anomalies = append(anomalies, map[string]interface{}{
				"type":           "variance_pattern",
				"severity":       severity,
				"worker":         worker,
				"short_rate":     shortRate,
				"total":          total,
				"short_count":    short,
				"total_variance": variance,
			})
		}
	}

	riskScore := 0
	for _, a := range anomalies {
		switch a["severity"] {
		case "low":
			riskScore += 5
		case "medium":
			riskScore += 15
		case "high":
			riskScore += 30
		case "critical":
			riskScore += 50
		}
	}
	if riskScore > 100 {
		riskScore = 100
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"shop_id":      shopID,
		"anomalies":    anomalies,
		"risk_score":   riskScore,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, email, role, name, active, created_at FROM shop_users WHERE shop_id = $1 ORDER BY created_at
	`, shopID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type user struct {
		ID        string    `json:"id"`
		Email     string    `json:"email"`
		Role      string    `json:"role"`
		Name      string    `json:"name"`
		Active    bool      `json:"active"`
		CreatedAt time.Time `json:"created_at"`
	}

	var users []user
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.Name, &u.Active, &u.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		users = append(users, u)
	}

	if users == nil {
		users = []user{}
	}

	respondJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		respondError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}

	if req.Role != "owner" && req.Role != "manager" && req.Role != "cashier" {
		respondError(w, http.StatusBadRequest, "role must be owner, manager, or cashier")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var id string
	err = s.db.QueryRowContext(r.Context(), `
		INSERT INTO shop_users (shop_id, email, password_hash, role, name) VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, shopID, req.Email, hash, req.Role, req.Name).Scan(&id)

	if err != nil {
		respondError(w, http.StatusConflict, "email already exists")
		return
	}

	slog.Info("user created", "id", id, "email", req.Email, "role", req.Role)
	respondCreated(w, map[string]string{"id": id, "email": req.Email, "role": req.Role})
}

func (s *Server) updateUserRole(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	userID := chi.URLParam(r, "id")

	var req struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Role != "owner" && req.Role != "manager" && req.Role != "cashier" {
		respondError(w, http.StatusBadRequest, "invalid role")
		return
	}

	_, err := s.db.ExecContext(r.Context(), `UPDATE shop_users SET role = $1 WHERE id = $2 AND shop_id = $3`, req.Role, userID, shopID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "update failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) disableUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	result, err := s.db.ExecContext(r.Context(), `UPDATE shop_users SET active = false WHERE id = $1`, userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "update failed")
		return
	}

	rows, _ := result.RowsAffected()
	respondJSON(w, http.StatusOK, map[string]interface{}{"status": "disabled", "affected": rows})
}

func computeEventHash(data []byte, previousHash string) string {
	content := string(data) + previousHash
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
