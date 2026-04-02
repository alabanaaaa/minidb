package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mini-database/core"
	coredb "mini-database/core/db"
	"mini-database/ledger"
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
	engine := &Engine{
		inventory:         NewInventoryService(),
		events:            []Event{},
		projectionManager: projection.NewManager(), // initialize manager to avoid nil panics
	}
	return engine
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
		inventory:         NewInventoryService(),
		db:                database,
		events:            []Event{},
		projectionManager: projection.NewManager(),
	}

	// register persistent projections
	engine.projectionManager.Register(
		projection.NewSalesProjection(),
	)

	if err := engine.loadEvents(); err != nil {
		database.Close()
		return nil, err
	}

	// Restore session from DB
	_ = engine.loadSession()

	return engine, nil
}

// Apply handles deterministic state transitions (used by replay AND runtime)
func (e *Engine) applyEvent(event Event) error {
	// Apply state changes based on event type. Do NOT persist here.
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
		if err := e.inventory.Reduce(sale.ProductID, sale.Quantity); err != nil {
			return err
		}
	default:
		// ignore unknown event types for now
	}
	return nil
}

// loadEvents reads indexed events (event:1, event:2...) and applies each event.
func (e *Engine) loadEvents() error {
	if e.db == nil {
		return nil
	}

	// Try to load from snapshot first for faster recovery
	startIndex := 0
	if snapIndex, err := e.loadSnapshot(); err == nil && snapIndex > 0 {
		startIndex = snapIndex
	}

	// Load index to know how many events exist
	indexStr, err := e.db.Get("event_index")
	if err != nil || indexStr == "" {
		return nil // No events yet
	}

	totalEvents, err := strconv.Atoi(indexStr)
	if err != nil {
		return err
	}

	// Clear in-memory and replay deterministically
	e.events = []Event{}

	for i := startIndex + 1; i <= totalEvents; i++ {
		key := fmt.Sprintf("event:%d", i)
		data, err := e.db.Get(key)
		if err != nil || data == "" {
			continue
		}

		var ev Event
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return err
		}

		if err := e.applyEvent(ev); err != nil {
			return err
		}
		e.events = append(e.events, ev)
	}

	return nil
}

func (e *Engine) ApplyStock(stock core.StockItem) error {
	// validate
	if err := stock.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(stock)
	if err != nil {
		return err
	}
	event := Event{
		Type:      "stock",
		Data:      data,
		Timestamp: time.Now(),
	}
	return e.appendEvent(event)
}

func (e *Engine) ApplySale(sale core.Sale) error {
	// validate
	if err := sale.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(sale)
	if err != nil {
		return err
	}
	event := Event{
		Type:      "sale",
		Data:      data,
		Timestamp: time.Now(),
	}
	return e.appendEvent(event)
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
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.appendEventLocked(evt)
}

func (e *Engine) appendEventLocked(evt Event) error {
	// set timestamp first
	evt.Timestamp = time.Now()

	// apply to in-memory state first (deterministic)
	if err := e.applyEvent(evt); err != nil {
		return err
	}

	// If no DB, just keep in-memory (for tests)
	if e.db == nil {
		if len(e.events) > 0 {
			evt.PreviousHash = e.events[len(e.events)-1].Hash
		} else {
			evt.PreviousHash = ""
		}
		evt.Hash = hashEvent(evt)
		e.events = append(e.events, evt)
		return nil
	}

	// Get last hash for chain
	lastHash, _ := e.db.Get(e.key("last_event_hash"))
	evt.PreviousHash = lastHash
	evt.Hash = hashEvent(evt)

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

	if index%100 == 0 {
		if err := e.SaveSnapshot(); err != nil {
			return fmt.Errorf("failed to save snapshot at event %d: %w", index, err)
		}
	}

	if err := e.db.Put(e.key("last_event_hash"), evt.Hash); err != nil {
		return err
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
	indexBytes, err := e.db.Get("event_index")
	if err != nil {
		return fmt.Errorf("failed to read event index for snapshot: %w", err)
	}
	if indexBytes == "" {
		return nil
	}

	index, err := strconv.Atoi(indexBytes)
	if err != nil {
		return fmt.Errorf("corrupted event index in snapshot: %w", err)
	}
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

func hashEvent(evt Event) string {
	return ledger.ComputeHashFromBytes(evt.Data, evt.PreviousHash)
}

func (e *Engine) AppendReplicatedEvent(evt Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Verify hash chain
	lastHash, err := e.db.Get(e.key("last_event_hash"))
	if err != nil {
		return fmt.Errorf("failed to read last hash: %w", err)
	}

	if evt.PreviousHash != lastHash {
		return fmt.Errorf("hash chain mismatch")
	}

	if evt.Hash != hashEvent(evt) {
		return fmt.Errorf("event tampered")
	}

	return e.appendEvent(evt)
}

func (e *Engine) ReplicateFrom(url string) error {
	indexBytes, err := e.db.Get(e.key("event_index"))
	if err != nil {
		return fmt.Errorf("failed to read event index: %w", err)
	}

	index, err := strconv.Atoi(string(indexBytes))
	if err != nil {
		index = 0
	}

	resp, err := http.Get(fmt.Sprintf("%s/events?after=%d", url, index))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var events []Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return fmt.Errorf("failed to decode events from remote: %w", err)
	}

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

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err

}

func (e *Engine) VerifyLedger() error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	prev := ""
	for i, ev := range e.events {
		// ev.Data is json.RawMessage (bytes) — use it directly
		expected := ledger.ComputeHashFromBytes(ev.Data, prev)
		if ev.Hash != expected {
			return fmt.Errorf("ledger corruption detected at event%d (hash mismatch)", i)
		}
		if i > 0 && ev.PreviousHash != prev {
			return fmt.Errorf("ledger chain broken at event%d (previous hash mismatch)", i)
		}
		prev = ev.Hash
	}
	return nil
}
