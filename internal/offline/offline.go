package offline

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	DBPath       string
	ServerURL    string
	SyncInterval time.Duration
	AutoSync     bool
}

type SyncStatus string

const (
	SyncStatusPending SyncStatus = "pending"
	SyncStatusSynced  SyncStatus = "synced"
	SyncStatusFailed  SyncStatus = "failed"
)

type SyncQueue struct {
	db        *sql.DB
	mu        sync.Mutex
	connected bool
	onConnect func() error
	quit      chan struct{}
}

type QueuedEvent struct {
	ID          int64     `json:"id"`
	EventType   string    `json:"event_type"`
	EventData   string    `json:"event_data"`
	CreatedAt   time.Time `json:"created_at"`
	Status      string    `json:"status"`
	RetryCount  int       `json:"retry_count"`
	LastAttempt time.Time `json:"last_attempt"`
}

func NewSyncQueue(cfg Config) (*SyncQueue, error) {
	db, err := sql.Open("sqlite3", cfg.DBPath+"?cache=shared")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	sq := &SyncQueue{
		db:        db,
		connected: true,
		quit:      make(chan struct{}),
	}

	go sq.healthCheck(cfg)

	return sq, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sync_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT NOT NULL,
		event_data TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		status TEXT DEFAULT 'pending',
		retry_count INTEGER DEFAULT 0,
		last_attempt TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS local_inventory (
		product_id TEXT PRIMARY KEY,
		quantity INTEGER NOT NULL,
		cost INTEGER NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		synced INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS local_sales (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		product_id TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		price INTEGER NOT NULL,
		worker_id TEXT NOT NULL,
		payment TEXT NOT NULL,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		status TEXT DEFAULT 'pending',
		receipt_id TEXT,
		synced INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS sync_metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(schema)
	return err
}

func (s *SyncQueue) QueueEvent(eventType string, data interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(
		"INSERT INTO sync_queue (event_type, event_data, status) VALUES (?, ?, ?)",
		eventType, string(dataJSON), SyncStatusPending,
	)
	return err
}

func (s *SyncQueue) GetPendingEvents() ([]QueuedEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id, event_type, event_data, created_at, status, retry_count, last_attempt
		FROM sync_queue
		WHERE status = 'pending' OR status = 'failed'
		ORDER BY created_at ASC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []QueuedEvent
	for rows.Next() {
		var e QueuedEvent
		var lastAttempt sql.NullTime
		if err := rows.Scan(&e.ID, &e.EventType, &e.EventData, &e.CreatedAt, &e.Status, &e.RetryCount, &lastAttempt); err != nil {
			continue
		}
		if lastAttempt.Valid {
			e.LastAttempt = lastAttempt.Time
		}
		events = append(events, e)
	}
	return events, nil
}

func (s *SyncQueue) MarkSynced(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("UPDATE sync_queue SET status = 'synced' WHERE id = ?", id)
	return err
}

func (s *SyncQueue) MarkFailed(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE sync_queue 
		SET status = 'failed', retry_count = retry_count + 1, last_attempt = ?
		WHERE id = ?`, time.Now(), id)
	return err
}

func (s *SyncQueue) SetConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
}

func (s *SyncQueue) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *SyncQueue) GetConnectionStatus() string {
	if s.IsConnected() {
		return "online"
	}
	return "offline"
}

func (s *SyncQueue) healthCheck(cfg Config) {
	ticker := time.NewTicker(cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.quit:
			return
		case <-ticker.C:
			if !s.IsConnected() {
				continue
			}

			if err := s.syncPendingEvents(cfg.ServerURL); err != nil {
				continue
			}
		}
	}
}

func (s *SyncQueue) syncPendingEvents(serverURL string) error {
	events, err := s.GetPendingEvents()
	if err != nil || len(events) == 0 {
		return nil
	}

	for _, e := range events {
		if err := s.syncEvent(serverURL, e); err != nil {
			s.MarkFailed(e.ID)
			continue
		}
		s.MarkSynced(e.ID)
	}

	return nil
}

func (s *SyncQueue) syncEvent(serverURL string, e QueuedEvent) error {
	endpoint := fmt.Sprintf("%s/api/sales", serverURL)

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(e.EventData), &payload); err != nil {
		return fmt.Errorf("unmarshal event data: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sync request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (s *SyncQueue) LocalInventory() (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT product_id, quantity FROM local_inventory WHERE quantity > 0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	inventory := make(map[string]int64)
	for rows.Next() {
		var productID string
		var quantity int64
		if err := rows.Scan(&productID, &quantity); err != nil {
			continue
		}
		inventory[productID] = quantity
	}
	return inventory, nil
}

func (s *SyncQueue) AddLocalStock(productID string, quantity, cost int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO local_inventory (product_id, quantity, cost, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(product_id) DO UPDATE SET
			quantity = quantity + excluded.quantity,
			updated_at = excluded.updated_at
	`, productID, quantity, cost, time.Now())
	return err
}

func (s *SyncQueue) RecordLocalSale(productID string, quantity, price int64, workerID, payment string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		INSERT INTO local_sales (product_id, quantity, price, worker_id, payment, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)
	`, productID, quantity, price, workerID, payment, time.Now())
	if err != nil {
		return 0, err
	}

	id, _ := result.LastInsertId()
	return id, nil
}

func (s *SyncQueue) GetLocalSales() ([]map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id, product_id, quantity, price, worker_id, payment, timestamp, status
		FROM local_sales
		WHERE status = 'pending'
		ORDER BY timestamp ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sales []map[string]interface{}
	for rows.Next() {
		var id int64
		var productID string
		var quantity, price int64
		var workerID, payment string
		var timestamp time.Time
		var status string

		if err := rows.Scan(&id, &productID, &quantity, &price, &workerID, &payment, &timestamp, &status); err != nil {
			continue
		}
		sales = append(sales, map[string]interface{}{
			"id":         id,
			"product_id": productID,
			"quantity":   quantity,
			"price":      price,
			"worker_id":  workerID,
			"payment":    payment,
			"timestamp":  timestamp,
			"status":     status,
		})
	}
	return sales, nil
}

func (s *SyncQueue) GetPendingCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sync_queue WHERE status = 'pending'").Scan(&count)
	return count, err
}

func (s *SyncQueue) Close() error {
	close(s.quit)
	return s.db.Close()
}
