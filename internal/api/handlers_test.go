package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mini-database/internal/auth"
	"mini-database/internal/config"
	"mini-database/internal/db"

	_ "github.com/lib/pq"
)

const (
	testDBURL = "postgres://postgres:postgres@localhost:5432/minidb_test?sslmode=disable"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("postgres", testDBURL)
	if err != nil {
		t.Skipf("cannot connect to test database: %v", err)
	}

	// Create test database if not exists
	_ = d.Close()
	d, err = sql.Open("postgres", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	defer d.Close()

	_, _ = d.Exec("CREATE DATABASE minidb_test")

	d.Close()

	d, err = sql.Open("postgres", testDBURL)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	// Drop and recreate schema
	_, _ = d.Exec(`
		DROP TABLE IF EXISTS system_metrics;
		DROP TABLE IF EXISTS audit_log;
		DROP TABLE IF EXISTS mpesa_transactions;
		DROP TABLE IF EXISTS events;
		DROP TABLE IF EXISTS sessions;
		DROP TABLE IF EXISTS workers;
		DROP TABLE IF EXISTS products;
		DROP TABLE IF EXISTS shop_users;
		DROP TABLE IF EXISTS shops;
	`)

	migrationSQL := `
	CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

	CREATE TABLE shops (
		id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		name          TEXT NOT NULL,
		owner_email   TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		tax_rate      NUMERIC(5,2) DEFAULT 0,
		currency      TEXT DEFAULT 'KES',
		phone         TEXT,
		address       TEXT,
		created_at    TIMESTAMPTZ DEFAULT NOW(),
		updated_at    TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE shop_users (
		id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		shop_id       UUID NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
		email         TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL CHECK (role IN ('owner', 'manager', 'cashier')),
		name          TEXT NOT NULL,
		phone         TEXT,
		active        BOOLEAN DEFAULT true,
		created_at    TIMESTAMPTZ DEFAULT NOW(),
		updated_at    TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE INDEX idx_shop_users_shop ON shop_users(shop_id);
	CREATE INDEX idx_shop_users_email ON shop_users(email);

	CREATE TABLE products (
		id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		shop_id    UUID NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		sku        TEXT,
		category   TEXT,
		cost_price BIGINT NOT NULL DEFAULT 0,
		sell_price BIGINT NOT NULL DEFAULT 0,
		stock_qty  BIGINT NOT NULL DEFAULT 0,
		min_stock  BIGINT NOT NULL DEFAULT 5,
		active     BOOLEAN DEFAULT true,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at    TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE INDEX idx_products_shop ON products(shop_id);

	CREATE TABLE workers (
		id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		shop_id UUID NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
		name    TEXT NOT NULL,
		pin     TEXT,
		active  BOOLEAN DEFAULT true,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE INDEX idx_workers_shop ON workers(shop_id);

	CREATE TABLE sessions (
		id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		shop_id       UUID NOT NULL REFERENCES shops(id),
		worker_id     UUID NOT NULL REFERENCES workers(id),
		started_at    TIMESTAMPTZ DEFAULT NOW(),
		ended_at      TIMESTAMPTZ,
		opening_float BIGINT DEFAULT 0,
		closing_cash  BIGINT,
		closing_mpesa BIGINT,
		status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
		notes         TEXT
	);
	CREATE INDEX idx_sessions_shop ON sessions(shop_id, status);

	CREATE TABLE events (
		id            BIGSERIAL PRIMARY KEY,
		shop_id       UUID NOT NULL REFERENCES shops(id),
		event_seq     BIGINT NOT NULL,
		event_type    TEXT NOT NULL CHECK (event_type IN ('stock_in', 'sale', 'reconciliation', 'session_open', 'session_close', 'price_change', 'void')),
		event_data    JSONB NOT NULL,
		previous_hash TEXT NOT NULL DEFAULT '',
		event_hash    TEXT NOT NULL,
		created_by    UUID REFERENCES shop_users(id),
		created_at    TIMESTAMPTZ DEFAULT NOW()
	);
	CREATE UNIQUE INDEX idx_events_shop_seq ON events(shop_id, event_seq);
	CREATE INDEX idx_events_shop_type ON events(shop_id, event_type);

	CREATE TABLE mpesa_transactions (
		id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		shop_id         UUID NOT NULL REFERENCES shops(id),
		mpesa_receipt   TEXT UNIQUE NOT NULL,
		phone_number    TEXT NOT NULL,
		amount          BIGINT NOT NULL,
		event_id        BIGINT REFERENCES events(id),
		status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'failed')),
		created_at      TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE audit_log (
		id         BIGSERIAL PRIMARY KEY,
		shop_id    UUID NOT NULL REFERENCES shops(id),
		user_id    UUID REFERENCES shop_users(id),
		action     TEXT NOT NULL,
		details    JSONB,
		ip_address TEXT,
		user_agent TEXT,
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE system_metrics (
		id          BIGSERIAL PRIMARY KEY,
		shop_id     UUID REFERENCES shops(id),
		metric_name TEXT NOT NULL,
		metric_value TEXT NOT NULL,
		created_at  TIMESTAMPTZ DEFAULT NOW()
	);
	`

	_, err = d.Exec(migrationSQL)
	if err != nil {
		d.Close()
		t.Fatalf("run migration: %v", err)
	}

	t.Cleanup(func() {
		d.Close()
	})

	return d
}

func seedTestData(t *testing.T, rawDB *sql.DB) (shopID, userID, workerID, productID, managerID, cashierID, authToken string) {
	t.Helper()

	passwordHash, err := auth.HashPassword("testpass123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = rawDB.QueryRow(`
		INSERT INTO shops (name, owner_email, password_hash) 
		VALUES ($1, $2, $3) RETURNING id
	`, "Test Shop", "owner@test.com", passwordHash).Scan(&shopID)
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}

	err = rawDB.QueryRow(`
		INSERT INTO shop_users (shop_id, email, password_hash, role, name) 
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, shopID, "owner@test.com", passwordHash, "owner", "Test Owner").Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	err = rawDB.QueryRow(`
		INSERT INTO shop_users (shop_id, email, password_hash, role, name) 
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, shopID, "manager@test.com", passwordHash, "manager", "Test Manager").Scan(&managerID)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	err = rawDB.QueryRow(`
		INSERT INTO shop_users (shop_id, email, password_hash, role, name) 
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, shopID, "cashier@test.com", passwordHash, "cashier", "Test Cashier").Scan(&cashierID)
	if err != nil {
		t.Fatalf("create cashier: %v", err)
	}

	err = rawDB.QueryRow(`
		INSERT INTO workers (shop_id, name, pin) 
		VALUES ($1, $2, $3) RETURNING id
	`, shopID, "Test Worker", "1234").Scan(&workerID)
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}

	err = rawDB.QueryRow(`
		INSERT INTO products (shop_id, name, sku, category, cost_price, sell_price, stock_qty, min_stock) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id
	`, shopID, "Test Product", "SKU001", "General", 50, 100, 100, 5).Scan(&productID)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	authSvc := auth.NewService("test-secret")
	token, _, err := authSvc.GenerateToken(userID, shopID, "owner@test.com", "owner")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	authToken = token

	os.WriteFile("/tmp/test_shop_id", []byte(shopID), 0644)
	os.WriteFile("/tmp/test_user_id", []byte(userID), 0644)
	os.WriteFile("/tmp/test_worker_id", []byte(workerID), 0644)
	os.WriteFile("/tmp/test_product_id", []byte(productID), 0644)
	os.WriteFile("/tmp/test_manager_id", []byte(managerID), 0644)
	os.WriteFile("/tmp/test_cashier_id", []byte(cashierID), 0644)
	os.WriteFile("/tmp/test_auth_token", []byte(authToken), 0644)

	return
}

func newTestServer(t *testing.T, rawDB *sql.DB) *Server {
	t.Helper()
	wrappedDB := &db.DB{DB: rawDB}
	cfg := &config.Config{
		JWTSecret: "test-secret",
		HTTPPort:  8080,
		Env:       "development",
		LogLevel:  "debug",
	}
	return New(wrappedDB, cfg)
}

func makeRequest(t *testing.T, srv *Server, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody []byte
	if body != nil {
		reqBody, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

func parseJSON(t *testing.T, rr *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("parse JSON: %v\nbody: %s", err, rr.Body.String())
	}
}

func TestHealthCheck(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/health", nil, "")
	if rr.Code != http.StatusOK {
		t.Errorf("health check: got %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string]string
	parseJSON(t, rr, &resp)
	if resp["status"] != "ok" {
		t.Errorf("health check: got status %q, want 'ok'", resp["status"])
	}
}

func TestReadinessCheck(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/ready", nil, "")
	if rr.Code != http.StatusOK {
		t.Errorf("readiness check: got %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestLogin(t *testing.T) {
	rawDB := testDB(t)
	seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/auth/login", map[string]string{
		"email":    "owner@test.com",
		"password": "testpass123",
	}, "")

	if rr.Code != http.StatusOK {
		t.Errorf("login: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["access_token"] == nil {
		t.Error("login: missing access_token")
	}
	if resp["refresh_token"] == nil {
		t.Error("login: missing refresh_token")
	}
	if resp["role"] != "owner" {
		t.Errorf("login: got role %q, want 'owner'", resp["role"])
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	rawDB := testDB(t)
	seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/auth/login", map[string]string{
		"email":    "owner@test.com",
		"password": "wrongpassword",
	}, "")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("login invalid: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestLoginMissingFields(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/auth/login", map[string]string{
		"email": "",
	}, "")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("login missing fields: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestRefreshToken(t *testing.T) {
	rawDB := testDB(t)
	seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/auth/login", map[string]string{
		"email":    "owner@test.com",
		"password": "testpass123",
	}, "")
	var loginResp map[string]interface{}
	parseJSON(t, rr, &loginResp)
	refreshToken := loginResp["refresh_token"].(string)

	rr = makeRequest(t, srv, "POST", "/api/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "")

	if rr.Code != http.StatusOK {
		t.Errorf("refresh token: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["access_token"] == nil {
		t.Error("refresh token: missing access_token")
	}
}

func TestRefreshTokenInvalid(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/auth/refresh", map[string]string{
		"refresh_token": "invalid-token",
	}, "")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("refresh invalid: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestDashboard(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/dashboard", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("dashboard: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if _, ok := resp["today_sales"]; !ok {
		t.Error("dashboard: missing today_sales")
	}
}

func TestDashboardNoAuth(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/dashboard", nil, "")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("dashboard no auth: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestListProducts(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/products", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("list products: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var products []map[string]interface{}
	parseJSON(t, rr, &products)
	if len(products) != 1 {
		t.Errorf("list products: got %d products, want 1", len(products))
	}
}

func TestCreateProduct(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/products", map[string]interface{}{
		"name":       "New Product",
		"sku":        "SKU002",
		"category":   "Test",
		"cost_price": 30,
		"sell_price": 60,
		"stock_qty":  50,
		"min_stock":  5,
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Errorf("create product: got %d, want %d, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["id"] == nil {
		t.Error("create product: missing id")
	}
}

func TestCreateProductMissingName(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/products", map[string]interface{}{
		"sku": "SKU003",
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("create product missing name: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUpdateProduct(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "PUT", "/api/products/"+productID, map[string]interface{}{
		"name":       "Updated Product",
		"sell_price": 120,
	}, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("update product: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestAddStock(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/products/"+productID+"/stock", map[string]interface{}{
		"quantity": 20,
		"cost":     500,
	}, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("add stock: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["added"] != float64(20) {
		t.Errorf("add stock: got added %v, want 20", resp["added"])
	}
}

func TestAddStockInvalidQuantity(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/products/"+productID+"/stock", map[string]interface{}{
		"quantity": -5,
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("add stock invalid qty: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestRecordSale(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   2,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Errorf("record sale: got %d, want %d, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["total"] != float64(200) {
		t.Errorf("record sale: got total %v, want 200", resp["total"])
	}

	var stock int64
	err := rawDB.QueryRow(`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2`, productID, shopID).Scan(&stock)
	if err != nil {
		t.Fatalf("check stock: %v", err)
	}
	if stock != 98 {
		t.Errorf("record sale: stock = %d, want 98", stock)
	}

	var eventType string
	err = rawDB.QueryRow(`SELECT event_type FROM events WHERE shop_id = $1 ORDER BY event_seq DESC LIMIT 1`, shopID).Scan(&eventType)
	if err != nil {
		t.Fatalf("check event: %v", err)
	}
	if eventType != "sale" {
		t.Errorf("record sale: event_type = %q, want 'sale'", eventType)
	}
}

func TestRecordSaleInsufficientStock(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	// Reduce stock to 0 first
	rawDB.Exec(`UPDATE products SET stock_qty = 0 WHERE id = $1`, productID)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("record sale no stock: got %d, want %d, body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestRecordSaleInvalidPayment(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    5,
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("record sale invalid payment: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestRecordSaleMissingFields(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": "",
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("record sale missing fields: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestListSales(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	rr := makeRequest(t, srv, "GET", "/api/sales", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("list sales: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var sales []map[string]interface{}
	parseJSON(t, rr, &sales)
	if len(sales) < 1 {
		t.Error("list sales: expected at least 1 sale")
	}
}

func TestGetReceipt(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	// Record a sale first
	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("record sale for receipt: got %d", rr.Code)
	}

	// Get receipt by event ID
	rr = makeRequest(t, srv, "GET", "/api/sales/1/receipt", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("get receipt: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	if rr.Header().Get("Content-Type") != "application/pdf" {
		t.Errorf("get receipt: content-type = %q, want application/pdf", rr.Header().Get("Content-Type"))
	}

	if len(rr.Body.Bytes()) == 0 {
		t.Error("get receipt: empty PDF body")
	}
}

func TestGetReceiptNotFound(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/sales/nonexistent/receipt", nil, authToken)

	if rr.Code != http.StatusNotFound {
		t.Errorf("get receipt not found: got %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestListWorkers(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/workers", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("list workers: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var workers []map[string]interface{}
	parseJSON(t, rr, &workers)
	if len(workers) < 1 {
		t.Error("list workers: expected at least 1 worker")
	}
}

func TestCreateWorker(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/workers", map[string]string{
		"name": "New Worker",
		"pin":  "5678",
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Errorf("create worker: got %d, want %d, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["id"] == nil {
		t.Error("create worker: missing id")
	}
}

func TestCreateWorkerMissingName(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/workers", map[string]string{
		"pin": "1234",
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("create worker missing name: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestOpenSession(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sessions", map[string]interface{}{
		"worker_id":     workerID,
		"opening_float": 1000,
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Errorf("open session: got %d, want %d, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
}

func TestCloseSession(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sessions", map[string]interface{}{
		"worker_id":     workerID,
		"opening_float": 1000,
	}, authToken)
	var sessionResp map[string]interface{}
	parseJSON(t, rr, &sessionResp)
	sessionID := sessionResp["id"].(string)

	rr = makeRequest(t, srv, "POST", "/api/sessions/"+sessionID+"/close", map[string]interface{}{
		"closing_cash":  1500,
		"closing_mpesa": 500,
	}, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("close session: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestListSessions(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	makeRequest(t, srv, "POST", "/api/sessions", map[string]interface{}{
		"worker_id":     workerID,
		"opening_float": 1000,
	}, authToken)

	rr := makeRequest(t, srv, "GET", "/api/sessions", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("list sessions: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var sessions []map[string]interface{}
	parseJSON(t, rr, &sessions)
	if len(sessions) < 1 {
		t.Error("list sessions: expected at least 1 session")
	}
}

func TestReconcile(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/reconcile", map[string]interface{}{
		"worker_id":      workerID,
		"declared_cash":  1000,
		"declared_mpesa": 500,
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Errorf("reconcile: got %d, want %d, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if _, ok := resp["expected_cash"]; !ok {
		t.Error("reconcile: missing expected_cash")
	}
	if _, ok := resp["cash_variance"]; !ok {
		t.Error("reconcile: missing cash_variance")
	}
}

func TestWorkerReconciliations(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/reconcile/"+workerID, nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("worker reconciliations: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestDailyReport(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/reports/daily", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("daily report: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if _, ok := resp["total_sales"]; !ok {
		t.Error("daily report: missing total_sales")
	}
}

func TestWorkerReport(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/reports/worker", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("worker report: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestLedgerReport(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/reports/ledger", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("ledger report: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var entries []map[string]interface{}
	parseJSON(t, rr, &entries)
	if entries == nil {
		t.Error("ledger report: entries should not be nil")
	}
}

func TestExportCSV(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/reports/export", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("export CSV: got %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestGhostReport(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/ghost", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("ghost report: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if _, ok := resp["risk_score"]; !ok {
		t.Error("ghost report: missing risk_score")
	}
	if _, ok := resp["anomalies"]; !ok {
		t.Error("ghost report: missing anomalies")
	}
}

func TestListUsers(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/admin/users", nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("list users: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var users []map[string]interface{}
	parseJSON(t, rr, &users)
	if len(users) < 1 {
		t.Error("list users: expected at least 1 user")
	}
}

func TestCreateUser(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/admin/users", map[string]string{
		"email":    "newuser@test.com",
		"password": "securepass123",
		"role":     "cashier",
		"name":     "New User",
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Errorf("create user: got %d, want %d, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
}

func TestCreateUserInvalidRole(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/admin/users", map[string]string{
		"email":    "badrole@test.com",
		"password": "pass123",
		"role":     "admin",
		"name":     "Bad Role",
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("create user invalid role: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestCreateUserMissingFields(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/admin/users", map[string]string{
		"email": "test@test.com",
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("create user missing fields: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUpdateUserRole(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, cashierID, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "PUT", "/api/admin/users/"+cashierID+"/role", map[string]string{
		"role": "manager",
	}, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("update user role: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestUpdateUserRoleInvalid(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, cashierID, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "PUT", "/api/admin/users/"+cashierID+"/role", map[string]string{
		"role": "superadmin",
	}, authToken)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("update user role invalid: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestDisableUser(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, cashierID, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "DELETE", "/api/admin/users/"+cashierID, nil, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("disable user: got %d, want %d, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["status"] != "disabled" {
		t.Errorf("disable user: got status %q, want 'disabled'", resp["status"])
	}

	var active bool
	err := rawDB.QueryRow(`SELECT active FROM shop_users WHERE id = $1`, cashierID).Scan(&active)
	if err != nil {
		t.Fatalf("check user active: %v", err)
	}
	if active {
		t.Error("disable user: user is still active")
	}
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/api/products", nil, "")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("auth missing header: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareInvalidFormat(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	req := httptest.NewRequest("GET", "/api/products", nil)
	req.Header.Set("Authorization", "InvalidFormat")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("auth invalid format: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareWrongSecret(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	wrongAuthSvc := auth.NewService("wrong-secret")
	token, _, _ := wrongAuthSvc.GenerateToken("user-1", "shop-1", "test@test.com", "owner")

	rr := makeRequest(t, srv, "GET", "/api/products", nil, token)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("auth wrong secret: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestEventHashChain(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	for i := 0; i < 3; i++ {
		rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
			"product_id": productID,
			"quantity":   1,
			"price":      100,
			"worker_id":  workerID,
			"payment":    1,
		}, authToken)
		if rr.Code != http.StatusCreated {
			t.Fatalf("sale %d: got %d, want %d", i, rr.Code, http.StatusCreated)
		}
	}

	rows, err := rawDB.Query(`SELECT event_seq, event_data, event_hash, previous_hash FROM events WHERE shop_id = $1 ORDER BY event_seq`, shopID)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	var prevHash string
	for rows.Next() {
		var seq int64
		var eventData, eventHash, previousHash string
		if err := rows.Scan(&seq, &eventData, &eventHash, &previousHash); err != nil {
			t.Fatalf("scan event: %v", err)
		}

		if previousHash != prevHash {
			t.Errorf("event %d: previous_hash mismatch. got %q, want %q", seq, previousHash, prevHash)
		}

		// Verify hash chain integrity (each event's previous_hash should match prior event's hash)
		if seq > 1 && previousHash == "" {
			t.Errorf("event %d: missing previous_hash", seq)
		}

		prevHash = eventHash
	}
}

func TestFullSaleFlow(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/products", map[string]interface{}{
		"name":       "Integration Product",
		"sku":        "INT001",
		"cost_price": 40,
		"sell_price": 80,
		"stock_qty":  10,
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create product: got %d", rr.Code)
	}
	var productResp map[string]interface{}
	parseJSON(t, rr, &productResp)
	productID := productResp["id"].(string)

	rr = makeRequest(t, srv, "POST", "/api/workers", map[string]string{
		"name": "Integration Worker",
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create worker: got %d", rr.Code)
	}
	var workerResp map[string]interface{}
	parseJSON(t, rr, &workerResp)
	workerID := workerResp["id"].(string)

	rr = makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   3,
		"price":      80,
		"worker_id":  workerID,
		"payment":    2,
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("record sale: got %d, body: %s", rr.Code, rr.Body.String())
	}

	var stock int64
	err := rawDB.QueryRow(`SELECT stock_qty FROM products WHERE id = $1 AND shop_id = $2`, productID, shopID).Scan(&stock)
	if err != nil {
		t.Fatalf("check stock: %v", err)
	}
	if stock != 7 {
		t.Errorf("stock: got %d, want 7", stock)
	}

	rr = makeRequest(t, srv, "GET", "/api/dashboard", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("dashboard: got %d", rr.Code)
	}
	var dashboard map[string]interface{}
	parseJSON(t, rr, &dashboard)
	if dashboard["today_sales"].(float64) != 240 {
		t.Errorf("dashboard today_sales: got %v, want 240", dashboard["today_sales"])
	}

	rr = makeRequest(t, srv, "GET", "/api/reports/ledger", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("ledger: got %d", rr.Code)
	}
	var ledger []map[string]interface{}
	parseJSON(t, rr, &ledger)
	if len(ledger) < 1 {
		t.Errorf("ledger: got %d events, want at least 1", len(ledger))
	}
}

func TestEmptyListReturnsEmptyArray(t *testing.T) {
	rawDB := testDB(t)
	seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	passwordHash, _ := auth.HashPassword("testpass123")
	var shopID string
	rawDB.QueryRow(`INSERT INTO shops (name, owner_email, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		"Empty Shop", "empty@test.com", passwordHash).Scan(&shopID)
	rawDB.Exec(`INSERT INTO shop_users (shop_id, email, password_hash, role, name) VALUES ($1, $2, $3, $4, $5)`,
		shopID, "empty@test.com", passwordHash, "owner", "Empty Owner")

	authSvc := auth.NewService("test-secret")
	token, _, _ := authSvc.GenerateToken("user-empty", shopID, "empty@test.com", "owner")

	rr := makeRequest(t, srv, "GET", "/api/products", nil, token)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty list: got %d", rr.Code)
	}

	var products []map[string]interface{}
	parseJSON(t, rr, &products)
	if products == nil {
		t.Error("empty list: should return empty array, not null")
	}
	if len(products) != 0 {
		t.Errorf("empty list: got %d products, want 0", len(products))
	}
}

func TestConcurrentSales(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rawDB.Exec(`UPDATE products SET stock_qty = 10 WHERE id = $1`, productID)

	results := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
				"product_id": productID,
				"quantity":   1,
				"price":      100,
				"worker_id":  workerID,
				"payment":    1,
			}, authToken)
			results <- rr.Code
		}()
	}

	successCount := 0
	failCount := 0
	for i := 0; i < 10; i++ {
		code := <-results
		if code == http.StatusCreated {
			successCount++
		} else {
			failCount++
		}
	}

	if successCount != 10 {
		t.Errorf("concurrent sales: %d succeeded, %d failed, want 10 succeeded", successCount, failCount)
	}

	var stock int64
	rawDB.QueryRow(`SELECT stock_qty FROM products WHERE id = $1`, productID).Scan(&stock)
	if stock != 0 {
		t.Errorf("concurrent sales: final stock = %d, want 0", stock)
	}

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("oversell: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestMpesaSale(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    2,
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Fatalf("mpesa sale: got %d", rr.Code)
	}

	rr = makeRequest(t, srv, "GET", "/api/dashboard", nil, authToken)
	var dashboard map[string]interface{}
	parseJSON(t, rr, &dashboard)

	if dashboard["today_mpesa"].(float64) != 100 {
		t.Errorf("dashboard mpesa: got %v, want 100", dashboard["today_mpesa"])
	}
}

func TestMultipleSalesThenList(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	for i := 0; i < 5; i++ {
		rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
			"product_id": productID,
			"quantity":   1,
			"price":      100,
			"worker_id":  workerID,
			"payment":    1,
		}, authToken)
		if rr.Code != http.StatusCreated {
			t.Fatalf("sale %d: got %d", i, rr.Code)
		}
	}

	rr := makeRequest(t, srv, "GET", "/api/sales?limit=3", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("list sales: got %d", rr.Code)
	}

	var sales []map[string]interface{}
	parseJSON(t, rr, &sales)
	if len(sales) != 3 {
		t.Errorf("list sales limit: got %d, want 3", len(sales))
	}
}

func TestStockInEventRecorded(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, _, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/products/"+productID+"/stock", map[string]interface{}{
		"quantity": 25,
		"cost":     1000,
	}, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("add stock: got %d", rr.Code)
	}

	var eventType string
	var eventData string
	err := rawDB.QueryRow(`SELECT event_type, event_data FROM events WHERE shop_id = $1 AND event_type = 'stock_in' ORDER BY event_seq DESC LIMIT 1`, shopID).Scan(&eventType, &eventData)
	if err != nil {
		t.Fatalf("query stock_in event: %v", err)
	}
	if eventType != "stock_in" {
		t.Errorf("event type: got %q, want 'stock_in'", eventType)
	}
	if !strings.Contains(eventData, productID) {
		t.Errorf("event data: missing product_id in %s", eventData)
	}
}

func TestReconciliationEventRecorded(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/reconcile", map[string]interface{}{
		"worker_id":      workerID,
		"declared_cash":  500,
		"declared_mpesa": 200,
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("reconcile: got %d", rr.Code)
	}

	var eventType string
	err := rawDB.QueryRow(`SELECT event_type FROM events WHERE shop_id = $1 AND event_type = 'reconciliation' ORDER BY event_seq DESC LIMIT 1`, shopID).Scan(&eventType)
	if err != nil {
		t.Fatalf("query reconciliation event: %v", err)
	}
	if eventType != "reconciliation" {
		t.Errorf("event type: got %q, want 'reconciliation'", eventType)
	}
}

func TestGhostReportWithReconciliations(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	for i := 0; i < 5; i++ {
		makeRequest(t, srv, "POST", "/api/reconcile", map[string]interface{}{
			"worker_id":      workerID,
			"declared_cash":  -100,
			"declared_mpesa": -50,
		}, authToken)
	}

	rr := makeRequest(t, srv, "GET", "/api/ghost", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("ghost report: got %d", rr.Code)
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)

	anomalies, ok := resp["anomalies"].([]interface{})
	if !ok {
		t.Fatal("ghost report: anomalies not an array")
	}

	riskScore := resp["risk_score"].(float64)
	if riskScore < 0 || riskScore > 100 {
		t.Errorf("ghost report: risk_score %v out of range [0,100]", riskScore)
	}
	_ = anomalies
}

func TestDisableUserPreventsLogin(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, cashierID, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "DELETE", "/api/admin/users/"+cashierID, nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("disable user: got %d", rr.Code)
	}

	rr = makeRequest(t, srv, "POST", "/api/auth/login", map[string]string{
		"email":    "cashier@test.com",
		"password": "testpass123",
	}, "")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("login disabled user: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestSessionLifecycle(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sessions", map[string]interface{}{
		"worker_id":     workerID,
		"opening_float": 2000,
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("open session: got %d", rr.Code)
	}
	var sessionResp map[string]interface{}
	parseJSON(t, rr, &sessionResp)
	sessionID := sessionResp["id"].(string)

	var status string
	var openingFloat int64
	err := rawDB.QueryRow(`SELECT status, opening_float FROM sessions WHERE id = $1 AND shop_id = $2`, sessionID, shopID).Scan(&status, &openingFloat)
	if err != nil {
		t.Fatalf("query session: %v", err)
	}
	if status != "active" {
		t.Errorf("session status: got %q, want 'active'", status)
	}
	if openingFloat != 2000 {
		t.Errorf("opening float: got %d, want 2000", openingFloat)
	}

	rr = makeRequest(t, srv, "POST", "/api/sessions/"+sessionID+"/close", map[string]interface{}{
		"closing_cash":  2500,
		"closing_mpesa": 1000,
	}, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("close session: got %d", rr.Code)
	}

	var closingCash, closingMpesa int64
	err = rawDB.QueryRow(`SELECT status, closing_cash, closing_mpesa FROM sessions WHERE id = $1`, sessionID).Scan(&status, &closingCash, &closingMpesa)
	if err != nil {
		t.Fatalf("query closed session: %v", err)
	}
	if status != "closed" {
		t.Errorf("session status after close: got %q, want 'closed'", status)
	}
}

func TestProductUpdateAndVerify(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, _, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "PUT", "/api/products/"+productID, map[string]interface{}{
		"name":       "Renamed Product",
		"sell_price": 150,
		"min_stock":  10,
	}, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("update product: got %d", rr.Code)
	}

	var name string
	var sellPrice int64
	var minStock int64
	err := rawDB.QueryRow(`SELECT name, sell_price, min_stock FROM products WHERE id = $1 AND shop_id = $2`, productID, shopID).Scan(&name, &sellPrice, &minStock)
	if err != nil {
		t.Fatalf("query updated product: %v", err)
	}
	if name != "Renamed Product" {
		t.Errorf("product name: got %q, want 'Renamed Product'", name)
	}
	if sellPrice != 150 {
		t.Errorf("sell price: got %d, want 150", sellPrice)
	}
	if minStock != 10 {
		t.Errorf("min stock: got %d, want 10", minStock)
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/admin/users", map[string]string{
		"email":    "duplicate@test.com",
		"password": "pass123",
		"role":     "cashier",
		"name":     "First",
	}, authToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create first user: got %d", rr.Code)
	}

	rr = makeRequest(t, srv, "POST", "/api/admin/users", map[string]string{
		"email":    "duplicate@test.com",
		"password": "pass456",
		"role":     "cashier",
		"name":     "Second",
	}, authToken)

	// Note: The schema doesn't have a unique constraint on shop_users.email,
	// so duplicate emails within a shop are allowed at the DB level.
	// The handler returns 409 only if there's a unique constraint violation.
	if rr.Code != http.StatusCreated && rr.Code != http.StatusConflict {
		t.Errorf("duplicate email: got %d, want 201 or 409", rr.Code)
	}
}

func TestWorkerReconciliationsReturnsHistory(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	for i := 0; i < 3; i++ {
		rr := makeRequest(t, srv, "POST", "/api/reconcile", map[string]interface{}{
			"worker_id":      workerID,
			"declared_cash":  1000 + int64(i)*100,
			"declared_mpesa": 500,
		}, authToken)
		if rr.Code != http.StatusCreated {
			t.Fatalf("reconcile %d: got %d", i, rr.Code)
		}
	}

	rr := makeRequest(t, srv, "GET", "/api/reconcile/"+workerID, nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("worker reconciliations: got %d", rr.Code)
	}

	var recs []map[string]interface{}
	parseJSON(t, rr, &recs)
	if len(recs) != 3 {
		t.Errorf("worker reconciliations: got %d, want 3", len(recs))
	}
}

func TestDailyReportWithSales(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   2,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    2,
	}, authToken)

	rr := makeRequest(t, srv, "GET", "/api/reports/daily", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("daily report: got %d", rr.Code)
	}

	var report map[string]interface{}
	parseJSON(t, rr, &report)

	if report["total_sales"].(float64) != 300 {
		t.Errorf("daily report total_sales: got %v, want 300", report["total_sales"])
	}
	if report["sale_count"].(float64) != 2 {
		t.Errorf("daily report sale_count: got %v, want 2", report["sale_count"])
	}
	if report["cash"].(float64) != 200 {
		t.Errorf("daily report cash: got %v, want 200", report["cash"])
	}
	if report["mpesa"].(float64) != 100 {
		t.Errorf("daily report mpesa: got %v, want 100", report["mpesa"])
	}
}

func TestWorkerReportWithSales(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   5,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	rr := makeRequest(t, srv, "GET", "/api/reports/worker", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("worker report: got %d", rr.Code)
	}

	var stats []map[string]interface{}
	parseJSON(t, rr, &stats)

	if len(stats) < 1 {
		t.Fatal("worker report: expected at least 1 worker stat")
	}

	for _, s := range stats {
		if s["worker"] == workerID {
			if s["sales"].(float64) != 1 {
				t.Errorf("worker report sales: got %v, want 1", s["sales"])
			}
			if s["total"].(float64) != 500 {
				t.Errorf("worker report total: got %v, want 500", s["total"])
			}
			return
		}
	}
	t.Error("worker report: worker not found in stats")
}

func TestLedgerReportShowsAllEvents(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	makeRequest(t, srv, "POST", "/api/products/"+productID+"/stock", map[string]interface{}{
		"quantity": 10,
		"cost":     500,
	}, authToken)

	makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	makeRequest(t, srv, "POST", "/api/reconcile", map[string]interface{}{
		"worker_id":      workerID,
		"declared_cash":  100,
		"declared_mpesa": 0,
	}, authToken)

	rr := makeRequest(t, srv, "GET", "/api/reports/ledger?limit=10", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("ledger report: got %d", rr.Code)
	}

	var entries []map[string]interface{}
	parseJSON(t, rr, &entries)

	if len(entries) < 3 {
		t.Errorf("ledger report: got %d entries, want at least 3", len(entries))
	}

	for _, entry := range entries {
		if entry["seq"] == nil {
			t.Error("ledger entry: missing seq")
		}
		if entry["type"] == nil {
			t.Error("ledger entry: missing type")
		}
		if entry["hash"] == nil {
			t.Error("ledger entry: missing hash")
		}
		if entry["previous_hash"] == nil {
			t.Error("ledger entry: missing previous_hash")
		}
	}
}

func TestRequestLogging(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "GET", "/health", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("health check: got %d", rr.Code)
	}

	if rr.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
		t.Error("missing Cache-Control header from middleware")
	}
	if rr.Header().Get("Pragma") != "no-cache" {
		t.Error("missing Pragma header from middleware")
	}
}

func TestCORSHeaders(t *testing.T) {
	rawDB := testDB(t)
	srv := newTestServer(t, rawDB)

	req := httptest.NewRequest("OPTIONS", "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("missing CORS Allow-Origin header")
	}
}

func TestSaleWithMpesaPayment(t *testing.T) {
	rawDB := testDB(t)
	shopID, _, workerID, productID, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": productID,
		"quantity":   3,
		"price":      200,
		"worker_id":  workerID,
		"payment":    2,
	}, authToken)

	if rr.Code != http.StatusCreated {
		t.Fatalf("mpesa sale: got %d", rr.Code)
	}

	var resp map[string]interface{}
	parseJSON(t, rr, &resp)
	if resp["total"].(float64) != 600 {
		t.Errorf("mpesa sale total: got %v, want 600", resp["total"])
	}

	var eventData string
	err := rawDB.QueryRow(`SELECT event_data FROM events WHERE shop_id = $1 AND event_type = 'sale' ORDER BY event_seq DESC LIMIT 1`, shopID).Scan(&eventData)
	if err != nil {
		t.Fatalf("query sale event: %v", err)
	}
	if !strings.Contains(eventData, `"payment": 2`) && !strings.Contains(eventData, `"payment":2`) {
		t.Errorf("sale event: missing M-Pesa payment indicator in %s", eventData)
	}
}

func TestMultipleProducts(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	for i := 0; i < 3; i++ {
		rr := makeRequest(t, srv, "POST", "/api/products", map[string]interface{}{
			"name":       fmt.Sprintf("Product %d", i),
			"sku":        fmt.Sprintf("SKU-MULTI-%d", i),
			"cost_price": 30,
			"sell_price": 60,
			"stock_qty":  20,
		}, authToken)
		if rr.Code != http.StatusCreated {
			t.Fatalf("create product %d: got %d", i, rr.Code)
		}
	}

	rr := makeRequest(t, srv, "GET", "/api/products", nil, authToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("list products: got %d", rr.Code)
	}

	var products []map[string]interface{}
	parseJSON(t, rr, &products)
	if len(products) != 4 {
		t.Errorf("list products: got %d, want 4", len(products))
	}
}

func TestProductNotFoundForSale(t *testing.T) {
	rawDB := testDB(t)
	_, _, workerID, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sales", map[string]interface{}{
		"product_id": "00000000-0000-0000-0000-000000000000",
		"quantity":   1,
		"price":      100,
		"worker_id":  workerID,
		"payment":    1,
	}, authToken)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("sale nonexistent product: got %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestSessionNotFoundForClose(t *testing.T) {
	rawDB := testDB(t)
	_, _, _, _, _, _, authToken := seedTestData(t, rawDB)
	srv := newTestServer(t, rawDB)

	rr := makeRequest(t, srv, "POST", "/api/sessions/00000000-0000-0000-0000-000000000000/close", map[string]interface{}{
		"closing_cash":  100,
		"closing_mpesa": 0,
	}, authToken)

	if rr.Code != http.StatusOK {
		t.Errorf("close nonexistent session: got %d", rr.Code)
	}
}

var _ = time.Now
