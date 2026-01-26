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

func TestDelete(t *testing.T) {
	path := "test_delete.db"
	_ = os.Remove(path)
	defer os.Remove(path)

	db, _ := OpenDB(path)
	defer db.Close()

	_ = db.Put("a", "1")
	_ = db.Delete("a")

	_, err := db.Get("a")
	if err == nil {
		t.Fatal("expected error for deleted key")
	}
}

func TestDeleteRecovery(t *testing.T) {
	path := "test_delete_recovery.db"
	_ = os.Remove(path)
	defer os.Remove(path)

	db, _ := OpenDB(path)
	_ = db.Put("a", "1")
	_ = db.Delete("a")
	db.Close()

	db2, _ := OpenDB(path)

	_, err := db2.Get("a")
	if err == nil {
		t.Fatal("expected deleted key after restart", err)
	}
}

func TestCompaction(t *testing.T) {
	path := "test_compact.db"
	os.Remove(path)

	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}

	db.Put("a", "1")
	db.Put("b", "2")
	db.Put("a", "3")
	db.Delete("b")

	if err := db.Compact(); err != nil {
		t.Fatal(err)
	}

	val, err := db.Get("a")
	if err != nil || val != "3" {
		t.Fatalf("expected a=3 got %v", val)
	}

	_, err = db.Get("b")
	if err == nil {
		t.Fatal("expected error for deleted key.")
	}
}
