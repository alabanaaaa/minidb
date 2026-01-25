package minidatabase

import (
	"os"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	path := "test.db"

	// Clean up before and after
	_ = os.Remove(path)
	defer os.Remove(path)

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	err = db.Put("name", "laban")
	if err != nil {
		t.Fatalf("put failed: %v", err)

	}

	val, err := db.Get("name")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if val != "laban" {
		t.Fatalf("Expected %q, got %q", "laban", val)
	}
}

func TestRecoveryAfterRestart(t *testing.T) {
	path := "test_recovery.db"

	// Clean up before and after
	_ = os.Remove(path)
	defer os.Remove(path)

	// First instance
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	if err := db.Put("language", "Go"); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	db.Close()

	// Second instance to test recovery
	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("failed to reopen db: %v", err)
	}
	defer db2.Close()

	val, err := db2.Get("language")
	if err != nil {
		t.Fatalf("get after recovery failed: %v", err)
	}

	if val != "Go" {
		t.Fatalf("Expected %q, got %q after recovery", "Go", val)
	}
}
