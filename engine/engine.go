package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mini-database/core"
	coredb "mini-database/core/db"
	"mini-database/projection"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jung-kurt/gofpdf"
)

type Event struct {
	Type         string          `json:"type"`
	Data         json.RawMessage `json:"data"`
	Timestamp    time.Time
	Hash         string `json:"hash"`
	PreviousHash string `json:"previous_hash"` // ...existing code...

}

type Engine struct {
	inventory         *InventoryService
	storeID           string
	db                *coredb.DB
	events            []Event // In-memory event log for reconciliation
	mu                sync.RWMutex
	session           *Session
	projectionManager *projection.Manager
}

type SalesSummary struct {
	TotalSales int64
	CashTotal  int64
	MpesaTotal int64
}

type Session struct {
	WorkerID  string    `json:"worker_id"`
	StartTime time.Time `json:"start_time"`
	Active    bool      `json:"active"`
}

type Snapshot struct {
	LastEventIndex int
	Inventory      map[string]int64
}

// In-memory only
func NewEngine() *Engine {
	return &Engine{
		inventory:         NewInventoryService(),
		events:            []Event{},
		projectionManager: nil, // Explicitly nil for in-memory
	}
}

// Persistent engine
func NewEngineWithDB(dbPath string) (*Engine, error) {
	database, err := coredb.OpenDB(dbPath)
	if err != nil {
		return nil, core.NewDomainErrorWithCause(
			core.ErrCodePersistence,
			"failed to open database",
			err,
		)
	}

	engine := &Engine{
		inventory: NewInventoryService(),
		db:        database,
		events:    []Event{},
	}

	// Initialize projection manager
	pm := projection.NewManager()
	pm.Register(
		projection.NewSalesProjection(database),
	)
	engine.projectionManager = pm

	if err := engine.replay(); err != nil {
		database.Close()
		return nil, err
	}

	return engine, nil
}

func (e *Engine) replay() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.db == nil {
		return nil
	}

	// Remove old array-based loading; use individual events
	return e.loadEvents() // Call the existing loadEvents method
}

// Apply handles deterministic state transitions (used by replay AND runtime)
func (e *Engine) applyEvent(event Event) error {
	switch event.Type {

	case "stock":
		var stock core.StockItem
		if err := json.Unmarshal(event.Data, &stock); err != nil {
			return err
		}
		e.inventory.Add(stock.ProductID, stock.Quantity)

	case "sale":
		var sale core.Sale
		if err := json.Unmarshal(event.Data, &sale); err != nil {
			return err
		}

		current := e.inventory.Get(sale.ProductID)
		if current < sale.Quantity {
			return core.NewDomainError(
				core.ErrCodeInsufficientStock,
				fmt.Sprintf("insufficient stock during replay for %s", sale.ProductID),
			)
		}

		if err := e.inventory.Reduce(sale.ProductID, sale.Quantity); err != nil {
			return err
		}

	case "reconciliation":
		// For now reconciliation does not mutate inventory
		// Future: track worker balances
		return nil

	default:
		return core.NewDomainError(
			core.ErrCodeInvalidOperation,
			fmt.Sprintf("unknown event type: %s", event.Type),
		)
	}

	return nil
}

func (e *Engine) persist(event Event) error {
	// Always track events in memory for reconciliation
	e.events = append(e.events, event)

	if e.db == nil {
		return nil
	}

	existing, _ := e.db.Get("__event_log__")

	var events []Event
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &events); err != nil {
			return err
		}
	}

	events = append(events, event)

	updated, err := json.Marshal(events)
	if err != nil {
		return err
	}

	return e.db.Put("__event_log__", string(updated))
}

func (e *Engine) ApplyStock(stock core.StockItem) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := stock.Validate(); err != nil {
		return err
	}

	data, _ := json.Marshal(stock)

	event := Event{
		Type:      "stock",
		Data:      data,
		Timestamp: time.Now(),
	}

	if err := e.applyEvent(event); err != nil {
		return err
	}

	return e.persist(event)
}

func (e *Engine) ApplySale(sale core.Sale) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := sale.Validate(); err != nil {
		return err
	}

	data, _ := json.Marshal(sale)

	event := Event{
		Type:      "sale",
		Data:      data,
		Timestamp: time.Now(),
	}

	if err := e.applyEvent(event); err != nil {
		return err
	}

	return e.persist(event)
}

func (e *Engine) GetStock(productID string) int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.inventory.Get(productID)
}

func (e *Engine) Close() error {
	if e.db != nil {
		return e.db.Close()
	}
	return nil
}

func (e *Engine) InventorySnapshot() map[string]int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.inventory.Snapshot()
}

func (e *Engine) SalesSummary() SalesSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var summary SalesSummary

	for _, evt := range e.events {
		if evt.Type != "sale" {
			continue
		}

		var sale core.Sale
		if err := json.Unmarshal(evt.Data, &sale); err != nil {
			continue
		}

		amount := sale.Price * sale.Quantity
		summary.TotalSales += amount

		switch sale.Payment {
		case core.PaymentCash:
			summary.CashTotal += amount
		case core.PaymentMpesa:
			summary.MpesaTotal += amount
		}
	}

	return summary
}

func (e *Engine) WorkerSummary(workerID string) SalesSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var summary SalesSummary

	for _, evt := range e.events {
		if evt.Type != "sale" {
			continue
		}

		var sale core.Sale
		if err := json.Unmarshal(evt.Data, &sale); err != nil {
			continue
		}

		if sale.WorkerID != workerID {
			continue
		}

		amount := sale.Price * sale.Quantity
		summary.TotalSales += amount

		switch sale.Payment {
		case core.PaymentCash:
			summary.CashTotal += amount
		case core.PaymentMpesa:
			summary.MpesaTotal += amount
		}
	}

	return summary
}

func (e *Engine) EventsByType(t string) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var filtered []Event

	for _, evt := range e.events {
		if evt.Type == t {
			filtered = append(filtered, evt)
		}
	}

	return filtered
}

func (e *Engine) AllEvents() []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return append([]Event(nil), e.events...)
}

func (e *Engine) PaginatedEvents(offset, limit int) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if offset > len(e.events) {
		return []Event{}
	}

	end := offset + limit
	if end > len(e.events) {
		end = len(e.events)
	}

	return append([]Event(nil), e.events[offset:end]...)
}

func (e *Engine) SalesSummaryWithRange(from, to time.Time) SalesSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var summary SalesSummary

	for _, evt := range e.events {

		if evt.Type != "sale" {
			continue
		}

		// Date filtering
		if !from.IsZero() && evt.Timestamp.Before(from) {
			continue
		}

		if !to.IsZero() && evt.Timestamp.After(to) {
			continue
		}

		var sale core.Sale
		if err := json.Unmarshal(evt.Data, &sale); err != nil {
			continue
		}

		amount := sale.Price * sale.Quantity
		summary.TotalSales += amount

		switch sale.Payment {
		case core.PaymentCash:
			summary.CashTotal += amount
		case core.PaymentMpesa:
			summary.MpesaTotal += amount
		}
	}

	return summary
}

func (e *Engine) StockSnapshotWithRange(from, to time.Time) map[string]int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tempInventory := make(map[string]int64)

	for _, evt := range e.events {

		if evt.Type != "stock" && evt.Type != "sale" {
			continue
		}

		if !from.IsZero() && evt.Timestamp.Before(from) {
			continue
		}

		if !to.IsZero() && evt.Timestamp.After(to) {
			continue
		}

		switch evt.Type {

		case "stock":
			var stock core.StockItem
			if err := json.Unmarshal(evt.Data, &stock); err != nil {
				continue
			}
			tempInventory[stock.ProductID] += stock.Quantity

		case "sale":
			var sale core.Sale
			if err := json.Unmarshal(evt.Data, &sale); err != nil {
				continue
			}
			tempInventory[sale.ProductID] -= sale.Quantity
		}
	}

	return tempInventory
}

func (e *Engine) appendEvent(evt Event) error {

	indexStr, _ := e.db.Get("event_index")

	var index int
	if indexStr != "" {
		index, _ = strconv.Atoi(indexStr)
	}

	index++

	key := fmt.Sprintf("event:%d", index)

	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	if err := e.db.Put(key, string(data)); err != nil {
		return err
	}

	if err := e.db.Put("event_index", strconv.Itoa(index)); err != nil {
		return err
	}

	e.events = append(e.events, evt)

	// Auto-snapshot every 100 events
	if index%100 == 0 {
		e.SaveSnapshot()
	}

	return nil
}

func (e *Engine) saveSession() error {
	if e.session == nil {
		return nil
	}

	data, err := json.Marshal(e.session)
	if err != nil {
		return err
	}

	return e.db.Put("active_session", string(data))
}

func (e *Engine) loadSession() error {

	data, err := e.db.Get("active_session")
	if err != nil || data == "" {
		return nil
	}

	var s Session
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return err
	}

	e.session = &s

	return nil
}

func (e *Engine) StartSession(worker string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.session = &Session{
		WorkerID:  worker,
		StartTime: time.Now(),
		Active:    true,
	}

	return e.saveSession()
}

func (e *Engine) ResumeSession() *Session {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.session
}

func (e *Engine) EndSession() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.session = nil

	return e.db.Delete("active_session")
}

func (e *Engine) SaveSnapshot() error {

	indexBytes, _ := e.db.Get("event_index")
	if indexBytes == "" {
		return nil
	}

	index, _ := strconv.Atoi(indexBytes)
	snapshot := Snapshot{
		LastEventIndex: index,
		Inventory:      e.inventory.items,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	return e.db.Put("snapshot", string(data))
}

func (e *Engine) loadSnapshot() (int, error) {

	data, err := e.db.Get("snapshot")
	if err != nil || data == "" {
		return 0, nil
	}

	var snap Snapshot
	if err := json.Unmarshal([]byte(data), &snap); err != nil {
		return 0, err
	}

	e.inventory.items = snap.Inventory

	return snap.LastEventIndex, nil
}

func (e *Engine) loadEvents() error {

	lastSnapIndex, _ := e.loadSnapshot()

	indexBytes, _ := e.db.Get("event_index")
	if indexBytes == "" {
		return nil
	}

	index, _ := strconv.Atoi(indexBytes)
	for i := lastSnapIndex + 1; i <= index; i++ {

		key := fmt.Sprintf("event:%d", i)
		data, _ := e.db.Get(key)
		if data == "" {
			continue
		}

		var evt Event
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		e.applyEvent(evt)
		e.events = append(e.events, evt)
	}

	return nil
}

func (e *Engine) EventsAfter(index int) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if index >= len(e.events) {
		return []Event{}
	}

	return e.events[index:]
}

// key is a helper method to generate database keys
func (e *Engine) key(suffix string) string {
	return suffix
}

// hashEvent computes the SHA256 hash of an event
func hashEvent(evt Event) string {
	data, _ := json.Marshal(evt)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (e *Engine) AppendReplicatedEvent(evt Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Verify hash chain
	lastHashBytes, _ := e.db.Get(e.key("last_event_hash"))
	lastHash := string(lastHashBytes)

	if evt.PreviousHash != lastHash {
		return fmt.Errorf("hash chain mismatch")
	}

	if evt.Hash != hashEvent(evt) {
		return fmt.Errorf("event tampered")
	}

	return e.appendEvent(evt)
}

func (e *Engine) ReplicateFrom(url string) error {
	indexBytes, _ := e.db.Get(e.key("event_index"))

	index, _ := strconv.Atoi(string(indexBytes))

	resp, err := http.Get(fmt.Sprintf("%s/events?after=%d", url, index))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var events []Event
	json.NewDecoder(resp.Body).Decode(&events)

	for _, evt := range events {
		if err := e.AppendReplicatedEvent(evt); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) ExportAudit(filename string) error {

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)

	for _, evt := range e.events {
		if err := encoder.Encode(evt); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) RecordSale(sale core.Sale) error {

	data, err := json.Marshal(sale)
	if err != nil {
		return err
	}

	evt := Event{
		Type:      "sale",
		Data:      data,
		Timestamp: time.Now(),
	}

	return e.appendEvent(evt)
}

func (e *Engine) EventCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.events)
}

func (e *Engine) GenerateReceipt(eventIndex int) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if eventIndex >= len(e.events) {
		return nil, fmt.Errorf("event index is invalid")

	}

	evt := e.events[eventIndex]

	if evt.Type != "sale" {
		return nil, fmt.Errorf("not a sale event")

	}

	var sale core.Sale
	if err := json.Unmarshal(evt.Data, &sale); err != nil {
		return nil, err
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "", 14)

	pdf.Cell(40, 10, "RECEIPT")
	pdf.Ln(12)

	pdf.Cell(40, 10, fmt.Sprintf("Quantity: %d", sale.Quantity))
	pdf.Ln(8)

	pdf.Cell(40, 10, fmt.Sprintf("Price: $%d", sale.Price))
	pdf.Ln(8)

	pdf.Cell(40, 10, fmt.Sprintf("Total: $%d", sale.Quantity*sale.Price))
	pdf.Ln(8)

	total := sale.Quantity * sale.Price
	pdf.Cell(40, 10, fmt.Sprintf("Total: $%d", total))

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err

}
