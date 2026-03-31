package engine

import (
	"mini-database/core"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistenceReplay(t *testing.T) {
	dir, err := os.MkdirTemp("", "minidb-test-*")
	if err != nil {
		t.Fatalf("tmp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Use a file path inside the temp directory (core DB expects a file, not a directory)
	dbPath := filepath.Join(dir, "minidb.db")

	e1, err := NewEngineWithDB(dbPath)
	if err != nil {
		t.Fatalf("NewEngineWithDB failed: %v", err)
	}

	if err := e1.ApplyStock(core.StockItem{ProductID: "banana", Quantity: 200}); err != nil {
		t.Fatalf("ApplyStock failed: %v", err)
	}
	if err := e1.ApplySale(core.Sale{ProductID: "banana", Quantity: 50, Price: 2, WorkerID: "w", Payment: core.PaymentCash}); err != nil {
		t.Fatalf("ApplySale failed: %v", err)
	}

	// capture state then close
	want := e1.GetStock("banana")
	e1.Close()

	// reopen and verify replay
	e2, err := NewEngineWithDB(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer e2.Close()

	got := e2.GetStock("banana")
	if got != want {
		t.Fatalf("replay mismatch: got %d want %d", got, want)
	}
}
