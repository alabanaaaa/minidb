package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectionManagersInitialized(t *testing.T) {
	// in-memory
	e := NewEngine()
	defer e.Close()
	if e.projectionManager == nil {
		t.Fatal("projectionManager should be initialized for in-memory engine")
	}

	// persistent
	dir, err := os.MkdirTemp("", "minidb-proj-*")
	if err != nil {
		t.Fatalf("tmp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "minidb.db")

	ep, err := NewEngineWithDB(dbPath)
	if err != nil {
		t.Fatalf("NewEngineWithDB failed: %v", err)
	}
	defer ep.Close()
	if ep.projectionManager == nil {
		t.Fatal("projectionManager should be initialized for persistent engine")
	}
}
