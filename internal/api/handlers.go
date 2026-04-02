package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mini-database/core"
	"mini-database/internal/auth"
	"mini-database/internal/email"
	mpesaPkg "mini-database/internal/mpesa"
	receiptPkg "mini-database/internal/receipt"

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
	clientIP := r.RemoteAddr
	if !s.loginLimiter.Allow(clientIP) {
		respondError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

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
	recordAudit(s.db.DB, r.Context(), shopID, userID, "login", "user", userID, r.RemoteAddr, r.UserAgent(), map[string]string{"email": email, "role": role})

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

	err := s.db.QueryRowContext(r.Context(), `
		SELECT 
			COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0),
			COUNT(*)
		FROM events 
		WHERE shop_id = $1 AND event_type = 'sale' 
		AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todaySales, &todaySaleCount)
	if err != nil {
		slog.Warn("dashboard sales query failed", "error", err)
	}

	err = s.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events 
		WHERE shop_id = $1 AND event_type = 'sale' 
		AND event_data->>'payment' = '1'
		AND created_at >= CURRENT_DATE
	`, shopID).Scan(&todayCash)
	if err != nil {
		slog.Warn("dashboard cash query failed", "error", err)
	}

	todayMpesa = todaySales - todayCash

	err = s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM products WHERE shop_id = $1 AND stock_qty <= min_stock AND active = true
	`, shopID).Scan(&lowStockCount)
	if err != nil {
		slog.Warn("dashboard low stock query failed", "error", err)
	}

	err = s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM sessions WHERE shop_id = $1 AND status = 'active'
	`, shopID).Scan(&activeSessions)
	if err != nil {
		slog.Warn("dashboard sessions query failed", "error", err)
	}

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

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 50
	}
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")

	query := `
		SELECT id, name, sku, category, cost_price, sell_price, stock_qty, min_stock, active, created_at
		FROM products WHERE shop_id = $1
	`
	args := []interface{}{shopID}
	argCount := 1

	if search != "" {
		argCount++
		query += fmt.Sprintf(" AND (name ILIKE $%d OR sku ILIKE $%d)", argCount, argCount)
		args = append(args, "%"+search+"%")
	}
	if category != "" {
		argCount++
		query += fmt.Sprintf(" AND category = $%d", argCount)
		args = append(args, category)
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM products WHERE shop_id = $1`
	countArgs := []interface{}{shopID}
	if search != "" {
		countQuery += " AND (name ILIKE $2 OR sku ILIKE $2)"
		countArgs = append(countArgs, "%"+search+"%")
	}
	if category != "" {
		countQuery += fmt.Sprintf(" AND category = $%d", len(countArgs)+1)
		countArgs = append(countArgs, category)
	}
	_ = s.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&total)

	query += fmt.Sprintf(" ORDER BY name LIMIT $%d OFFSET $%d", argCount+1, argCount+2)
	args = append(args, limit, (page-1)*limit)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
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
	userID := GetUserID(r)
	recordAudit(s.db.DB, r.Context(), shopID, userID, "product_created", "product", id, r.RemoteAddr, r.UserAgent(), map[string]string{"name": req.Name, "sku": req.SKU})

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
		err = tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(event_hash, '') FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`,
			shopID,
		).Scan(&lastHash)
		if err != nil {
			slog.Warn("failed to get last hash for stock event", "error", err)
		}

		var seq int64
		err = tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(event_seq), 0) FROM events WHERE shop_id = $1`,
			shopID,
		).Scan(&seq)
		if err != nil {
			slog.Warn("failed to get sequence number for stock event", "error", err)
		}
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

	// Also record in engine for dual-path consistency
	stockItem := core.StockItem{
		ProductID: productID,
		Quantity:  req.Quantity,
	}
	if engErr := s.engine.ApplyStock(stockItem); engErr != nil {
		slog.Warn("failed to record stock in engine", "error", engErr)
	}

	slog.Info("stock added", "product", productID, "qty", req.Quantity, "shop", shopID)
	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "stock_added", "product", productID, r.RemoteAddr, r.UserAgent(), map[string]interface{}{"quantity": req.Quantity, "cost": req.Cost})
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
		err = tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(event_hash, '') FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`,
			shopID,
		).Scan(&lastHash)
		if err != nil {
			slog.Warn("failed to get last hash for sale event", "error", err)
		}

		var seq int64
		err = tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(event_seq), 0) FROM events WHERE shop_id = $1`,
			shopID,
		).Scan(&seq)
		if err != nil {
			slog.Warn("failed to get sequence number for sale event", "error", err)
		}
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
		if strings.HasPrefix(err.Error(), "insufficient stock") {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to record sale")
		return
	}

	// Also record in engine for dual-path consistency
	sale := core.Sale{
		ProductID: req.ProductID,
		Quantity:  req.Quantity,
		Price:     req.Price,
		WorkerID:  req.WorkerID,
		Payment:   core.PaymentMethod(req.Payment),
		TimeStamp: time.Now(),
	}
	if engErr := s.engine.RecordSale(sale); engErr != nil {
		slog.Warn("failed to record sale in engine", "error", engErr)
	}

	slog.Info("sale recorded", "product", req.ProductID, "qty", req.Quantity, "total", req.Price*req.Quantity, "shop", shopID)
	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "sale_recorded", "sale", "", r.RemoteAddr, r.UserAgent(), map[string]interface{}{"product": req.ProductID, "quantity": req.Quantity, "total": req.Price * req.Quantity, "payment": req.Payment})

	respondCreated(w, map[string]interface{}{
		"status":   "ok",
		"product":  req.ProductID,
		"quantity": req.Quantity,
		"total":    req.Price * req.Quantity,
	})
}

func (s *Server) listSales(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	workerID := r.URL.Query().Get("worker_id")

	query := `
		SELECT id, event_seq, event_data, created_at
		FROM events WHERE shop_id = $1 AND event_type = 'sale'
	`
	args := []interface{}{shopID}
	argCount := 1

	if from != "" {
		argCount++
		query += fmt.Sprintf(" AND created_at >= $%d", argCount)
		args = append(args, from)
	}
	if to != "" {
		argCount++
		query += fmt.Sprintf(" AND created_at <= $%d", argCount)
		args = append(args, to)
	}
	if workerID != "" {
		argCount++
		query += fmt.Sprintf(" AND event_data->>'worker_id' = $%d", argCount)
		args = append(args, workerID)
	}

	query += fmt.Sprintf(" ORDER BY event_seq DESC LIMIT $%d OFFSET $%d", argCount+1, argCount+2)
	args = append(args, limit, (page-1)*limit)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
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
	eventID := chi.URLParam(r, "id")
	shopID := GetShopID(r)

	var seq int64
	var dataStr string
	var createdAt time.Time
	var hash string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT event_seq, event_data, created_at, event_hash
		FROM events WHERE shop_id = $1 AND (id::text = $2 OR event_seq::text = $2)
	`, shopID, eventID).Scan(&seq, &dataStr, &createdAt, &hash)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "receipt not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var saleData map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &saleData); err != nil {
		respondError(w, http.StatusInternalServerError, "parse event data")
		return
	}

	productID, _ := saleData["product_id"].(string)
	quantity, _ := saleData["quantity"].(float64)
	price, _ := saleData["price"].(float64)
	workerID, _ := saleData["worker_id"].(string)
	paymentVal, _ := saleData["payment"].(float64)

	paymentMethod := "Cash"
	if paymentVal == 2 {
		paymentMethod = "M-Pesa"
	}

	total := int64(price * quantity)

	var productName string
	s.db.QueryRowContext(r.Context(), `SELECT name FROM products WHERE id = $1`, productID).Scan(&productName)
	if productName == "" {
		productName = productID
	}

	var workerName string
	s.db.QueryRowContext(r.Context(), `SELECT name FROM workers WHERE id = $1`, workerID).Scan(&workerName)
	if workerName == "" {
		workerName = workerID
	}

	var shopName, currency string
	s.db.QueryRowContext(r.Context(), `SELECT name, currency FROM shops WHERE id = $1`, shopID).Scan(&shopName, &currency)
	if shopName == "" {
		shopName = "Shop"
	}
	if currency == "" {
		currency = "KES"
	}

	receipt := receiptPkg.Receipt{
		ShopName:  shopName,
		ReceiptNo: fmt.Sprintf("RCP-%d-%d", seq, createdAt.Unix()),
		Date:      createdAt,
		Items: []receiptPkg.ReceiptItem{
			{
				Name:     productName,
				Quantity: int64(quantity),
				Price:    int64(price),
				Total:    total,
			},
		},
		Subtotal: total,
		Total:    total,
		Payment:  paymentMethod,
		Worker:   workerName,
		Currency: currency,
	}

	pdf, err := receiptPkg.GeneratePDF(receipt)
	if err != nil {
		slog.Error("receipt PDF generation failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to generate receipt")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=receipt-%d.pdf", seq))
	w.Write(pdf)
}

func (s *Server) listWorkers(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 50
	}
	search := r.URL.Query().Get("search")

	query := `SELECT id, name, pin IS NOT NULL as has_pin, active, created_at FROM workers WHERE shop_id = $1`
	args := []interface{}{shopID}
	argCount := 1

	if search != "" {
		argCount++
		query += fmt.Sprintf(" AND name ILIKE $%d", argCount)
		args = append(args, "%"+search+"%")
	}

	query += fmt.Sprintf(" ORDER BY name LIMIT $%d OFFSET $%d", argCount+1, argCount+2)
	args = append(args, limit, (page-1)*limit)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
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

	var pinHash string
	if req.PIN != "" {
		var err error
		pinHash, err = auth.HashPassword(req.PIN)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to hash PIN")
			return
		}
	}

	var id string
	err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO workers (shop_id, name, pin) VALUES ($1, $2, $3) RETURNING id
	`, shopID, req.Name, pinHash).Scan(&id)

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create worker")
		return
	}

	slog.Info("worker created", "id", id, "name", req.Name, "shop", shopID)
	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "worker_created", "worker", id, r.RemoteAddr, r.UserAgent(), map[string]string{"name": req.Name})
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
	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "session_opened", "session", id, r.RemoteAddr, r.UserAgent(), map[string]string{"worker_id": req.WorkerID})
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
	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "session_closed", "session", sessionID, r.RemoteAddr, r.UserAgent(), nil)
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
	err = s.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(event_hash, '') FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`,
		shopID,
	).Scan(&lastHash)
	if err != nil {
		slog.Warn("failed to get last hash for reconciliation event", "error", err)
	}

	var seq int64
	err = s.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(event_seq), 0) FROM events WHERE shop_id = $1`,
		shopID,
	).Scan(&seq)
	if err != nil {
		slog.Warn("failed to get sequence number for reconciliation event", "error", err)
	}
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

	if _, engErr := s.engine.Reconcile(req.WorkerID, req.DeclaredCash, req.DeclaredMpesa); engErr != nil {
		slog.Warn("failed to record reconciliation in engine", "error", engErr)
	}

	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "reconciliation", "reconciliation", "", r.RemoteAddr, r.UserAgent(), map[string]interface{}{
		"worker_id": req.WorkerID, "declared_cash": req.DeclaredCash, "declared_mpesa": req.DeclaredMpesa,
		"expected_cash": expectedCash, "expected_mpesa": expectedMpesa,
	})

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
	shopID := GetShopID(r)
	exportType := r.URL.Query().Get("type")
	if exportType == "" {
		exportType = "sales"
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	switch exportType {
	case "sales":
		writer.Write([]string{"seq", "product_id", "quantity", "price", "total", "worker_id", "payment", "created_at"})
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT event_seq, event_data, created_at
			FROM events WHERE shop_id = $1 AND event_type = 'sale'
			ORDER BY event_seq ASC
		`, shopID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()

		for rows.Next() {
			var seq int64
			var dataStr string
			var createdAt time.Time
			if err := rows.Scan(&seq, &dataStr, &createdAt); err != nil {
				continue
			}
			var saleData map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &saleData); err != nil {
				continue
			}
			productID, _ := saleData["product_id"].(string)
			quantity, _ := saleData["quantity"].(float64)
			price, _ := saleData["price"].(float64)
			workerID, _ := saleData["worker_id"].(string)
			payment, _ := saleData["payment"].(float64)
			total := price * quantity
			paymentStr := "cash"
			if payment == 2 {
				paymentStr = "mpesa"
			}
			writer.Write([]string{
				fmt.Sprintf("%d", seq),
				productID,
				fmt.Sprintf("%.0f", quantity),
				fmt.Sprintf("%.0f", price),
				fmt.Sprintf("%.0f", total),
				workerID,
				paymentStr,
				createdAt.Format(time.RFC3339),
			})
		}

	case "inventory":
		writer.Write([]string{"id", "name", "sku", "category", "cost_price", "sell_price", "stock_qty", "min_stock", "active"})
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT id, name, sku, category, cost_price, sell_price, stock_qty, min_stock, active
			FROM products WHERE shop_id = $1 ORDER BY name
		`, shopID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id, name, sku, category string
			var costPrice, sellPrice, stockQty, minStock int64
			var active bool
			if err := rows.Scan(&id, &name, &sku, &category, &costPrice, &sellPrice, &stockQty, &minStock, &active); err != nil {
				continue
			}
			activeStr := "true"
			if !active {
				activeStr = "false"
			}
			writer.Write([]string{id, name, sku, category, fmt.Sprintf("%d", costPrice), fmt.Sprintf("%d", sellPrice), fmt.Sprintf("%d", stockQty), fmt.Sprintf("%d", minStock), activeStr})
		}

	case "workers":
		writer.Write([]string{"id", "name", "active", "created_at"})
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT id, name, active, created_at FROM workers WHERE shop_id = $1 ORDER BY name
		`, shopID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id, name string
			var active bool
			var createdAt time.Time
			if err := rows.Scan(&id, &name, &active, &createdAt); err != nil {
				continue
			}
			activeStr := "true"
			if !active {
				activeStr = "false"
			}
			writer.Write([]string{id, name, activeStr, createdAt.Format(time.RFC3339)})
		}

	default:
		respondError(w, http.StatusBadRequest, "unknown export type: "+exportType)
		return
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		slog.Error("CSV write error", "error", err)
		respondError(w, http.StatusInternalServerError, "CSV generation failed")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.csv", exportType, time.Now().Format("2006-01-02")))
	w.Write(buf.Bytes())
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
	recordAudit(s.db.DB, r.Context(), shopID, GetUserID(r), "user_created", "user", id, r.RemoteAddr, r.UserAgent(), map[string]string{"email": req.Email, "role": req.Role})
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

func (s *Server) mpesaPay(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)

	var req struct {
		PhoneNumber string `json:"phone_number"`
		ProductID   string `json:"product_id"`
		Quantity    int64  `json:"quantity"`
		Price       int64  `json:"price"`
		WorkerID    string `json:"worker_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PhoneNumber == "" || req.ProductID == "" || req.Quantity <= 0 || req.Price < 0 || req.WorkerID == "" {
		respondError(w, http.StatusBadRequest, "phone_number, product_id, quantity (>0), price (>=0), and worker_id are required")
		return
	}

	if s.mpesaClient == nil {
		respondError(w, http.StatusServiceUnavailable, "M-Pesa not configured")
		return
	}

	total := req.Price * req.Quantity
	accountRef := mpesaPkg.GenerateAccountRef()

	resp, err := s.mpesaClient.STKPush(mpesaPkg.STKPushRequest{
		PhoneNumber: req.PhoneNumber,
		Amount:      total,
		AccountRef:  accountRef,
		Description: fmt.Sprintf("POS Purchase - %s x%d", req.ProductID, req.Quantity),
	})
	if err != nil {
		slog.Error("STK push failed", "error", err)
		respondError(w, http.StatusPaymentRequired, "Payment initiation failed. Please try again.")
		return
	}

	if resp.ResponseCode != "0" {
		slog.Error("STK push returned error", "code", resp.ResponseCode, "desc", resp.ResponseDesc)
		respondError(w, http.StatusPaymentRequired, resp.ResponseDesc)
		return
	}

	slog.Info("M-Pesa STK push initiated",
		"checkout_id", resp.CheckoutRequestID,
		"amount", total,
		"phone", req.PhoneNumber,
		"shop", shopID,
	)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"checkout_id": resp.CheckoutRequestID,
		"message":     "Payment initiated. Check your phone for STK push.",
		"amount":      total,
	})
}

func (s *Server) mpesaCallback(w http.ResponseWriter, r *http.Request) {
	var callback struct {
		Body struct {
			STKCallback struct {
				ResultCode       int    `json:"ResultCode"`
				ResultDesc       string `json:"ResultDesc"`
				CallbackMetadata struct {
					Item []struct {
						Name  string      `json:"Name"`
						Value interface{} `json:"Value"`
					} `json:"Item"`
				} `json:"CallbackMetadata"`
			} `json:"stkCallback"`
		} `json:"Body"`
	}

	if err := json.NewDecoder(r.Body).Decode(&callback); err != nil {
		slog.Error("parse M-Pesa callback failed", "error", err)
		http.Error(w, "invalid callback", http.StatusBadRequest)
		return
	}

	resultCode := callback.Body.STKCallback.ResultCode
	success := resultCode == 0

	var mpesaReceipt, phoneNumber, transactionID string
	var amount float64

	for _, item := range callback.Body.STKCallback.CallbackMetadata.Item {
		switch item.Name {
		case "MpesaReceiptNumber":
			if v, ok := item.Value.(string); ok {
				mpesaReceipt = v
			}
		case "PhoneNumber":
			if v, ok := item.Value.(float64); ok {
				phoneNumber = fmt.Sprintf("%.0f", v)
			}
		case "Amount":
			if v, ok := item.Value.(float64); ok {
				amount = v
			}
		case "TransactionDate":
		case "TransactionId":
			if v, ok := item.Value.(string); ok {
				transactionID = v
			}
		}
	}

	if !success {
		slog.Info("M-Pesa payment cancelled or failed",
			"code", resultCode,
			"desc", callback.Body.STKCallback.ResultDesc,
		)
		respondJSON(w, http.StatusOK, map[string]string{"status": "failed"})
		return
	}

	slog.Info("M-Pesa payment successful",
		"receipt", mpesaReceipt,
		"amount", amount,
		"phone", phoneNumber,
	)

	respondJSON(w, http.StatusOK, map[string]string{
		"status":         "success",
		"receipt":        mpesaReceipt,
		"transaction_id": transactionID,
	})
}

func computeEventHash(data []byte, previousHash string) string {
	content := string(data) + previousHash
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func recordAudit(db *sql.DB, ctx context.Context, shopID, userID, action, entityType, entityID, ipAddress, userAgent string, details interface{}) {
	detailsJSON, _ := json.Marshal(details)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO audit_log (shop_id, user_id, action, details, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, shopID, userID, action, string(detailsJSON), ipAddress, userAgent)
}

func (s *Server) sendDailyReport(shopID string) {
	var shopName, ownerEmail, currency string
	err := s.db.QueryRowContext(context.Background(), `SELECT name, owner_email, currency FROM shops WHERE id = $1`, shopID).Scan(&shopName, &ownerEmail, &currency)
	if err != nil {
		return
	}

	var totalSales, totalCash, totalMpesa int64
	var saleCount int
	_ = s.db.QueryRowContext(context.Background(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0),
			COUNT(*)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&totalSales, &saleCount)
	_ = s.db.QueryRowContext(context.Background(), `
		SELECT COALESCE(SUM((event_data->>'price')::bigint * (event_data->>'quantity')::bigint), 0)
		FROM events WHERE shop_id = $1 AND event_type = 'sale' AND event_data->>'payment' = '1' AND created_at >= CURRENT_DATE
	`, shopID).Scan(&totalCash)
	totalMpesa = totalSales - totalCash

	rows, err := s.db.QueryContext(context.Background(), `
		SELECT w.name, COUNT(e.id), COALESCE(SUM((e.event_data->>'price')::bigint * (e.event_data->>'quantity')::bigint), 0)
		FROM events e JOIN workers w ON (e.event_data->>'worker_id')::uuid = w.id
		WHERE e.shop_id = $1 AND e.event_type = 'sale' AND e.created_at >= CURRENT_DATE
		GROUP BY w.name
	`, shopID)
	if err != nil {
		return
	}
	defer rows.Close()

	var workerStats []email.WorkerStat
	for rows.Next() {
		var name string
		var sales int
		var total int64
		if err := rows.Scan(&name, &sales, &total); err != nil {
			continue
		}
		workerStats = append(workerStats, email.WorkerStat{Name: name, Sales: sales, Total: total})
	}

	report := email.ReportEmail{
		To:          ownerEmail,
		ShopName:    shopName,
		Date:        time.Now().Format("2006-01-02"),
		TotalSales:  totalSales,
		TotalCash:   totalCash,
		TotalMpesa:  totalMpesa,
		SaleCount:   saleCount,
		Currency:    currency,
		WorkerStats: workerStats,
	}

	emailCfg := email.Config{
		SMTPHost:  s.cfg.Email.SMTPHost,
		SMTPPort:  s.cfg.Email.SMTPPort,
		SMTPUser:  s.cfg.Email.SMTPUser,
		SMTPPass:  s.cfg.Email.SMTPPass,
		FromEmail: s.cfg.Email.FromEmail,
		FromName:  s.cfg.Email.FromName,
	}

	if err := email.SendReport(emailCfg, report); err != nil {
		slog.Error("failed to send daily report", "shop", shopID, "error", err)
	}
}

func (s *Server) sendReportEmail(w http.ResponseWriter, r *http.Request) {
	shopID := GetShopID(r)
	userID := GetUserID(r)

	go s.sendDailyReport(shopID)

	recordAudit(s.db.DB, r.Context(), shopID, userID, "report_email_sent", "report", "", r.RemoteAddr, r.UserAgent(), nil)
	respondJSON(w, http.StatusOK, map[string]string{"status": "report email queued"})
}
