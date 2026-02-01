package minidatabase

import (
	"path/filepath"
	"testing"
)

func NewTestDB(t *testing.T, name string) *DB {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

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
	dir := t.TempDir()
	path := filepath.Join(dir, "test_recovery.db")

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
	dir := t.TempDir()
	path := filepath.Join(dir, "test_delete.db")

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
	dir := t.TempDir()
	path := filepath.Join(dir, "test_delete_recovery.db")

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
	dir := t.TempDir()
	path := filepath.Join(dir, "test_compact.db")

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

func TestOverWriteValue(t *testing.T) {
	db := NewTestDB(t, "overwrite.db")

	if err := db.Put("a", "1"); err != nil {
		t.Fatal(err)
	}

	if err := db.Put("a", "2"); err != nil {
		t.Fatal(err)
	}

	val, err := db.Get("a")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if val != "2" {
		t.Fatalf("Expected %q, got %q", "2", val)
	}
}

func TestDeleteThenPut(t *testing.T) {
	db := NewTestDB(t, "delete_put.db")

	db.Put("x", "old")
	db.Delete("x")
	db.Put("x", "new")

	val, err := db.Get("x")

	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if val != "new" {
		t.Fatalf("expected %q, got %q", "new", val)
	}
}

func TestDeleteNonExistentKey(t *testing.T) {
	db := NewTestDB(t, "delete_missing.db")

	if err := db.Delete("ghost"); err != nil {
		t.Fatalf("delete failed: %v", err)

	}

	_, err := db.Get("ghost")
	if err == nil {
		t.Fatalf("expected error for deleted key")
	}
}

func TestMixedKeysRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.db")

	db, _ := OpenDB(path)
	db.Put("a", "1")
	db.Put("b", "2")
	db.Put("c", "3")
	db.Delete("b")
	db.Close()

	db2, _ := OpenDB(path)
	defer db2.Close()

	val, _ := db2.Get("a")
	if val != "1" {
		t.Fatalf("expected 1, got %s", val)
	}

	_, err := db2.Get("b")
	if err == nil {
		t.Fatalf("expected b to be deleted")

	}

	val, _ = db2.Get("c")
	if val != "3" {
		t.Fatalf("expected 3, got %s", val)
	}
}

func TestEmptyValue(t *testing.T) {
	db := NewTestDB(t, "empty.db")

	db.Put("empty", "")

	val, err := db.Get("empty")
	if err != nil {
		t.Fatalf("get failed %v", err)
	}

	if val != "" {
		t.Fatalf("expected empty string, got %q", val)
	}
}

func TestLargeValue(t *testing.T) {
	db := NewTestDB(t, "large.db")

	large := make([]byte, 1024*1024) // 1MB
	for i := range large {
		large[i] = 'a'
	}

	db.Put("big", string(large))

	val, err := db.Get("big")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if len(val) != len(large) {
		t.Fatalf("size mismatch")
	}
}

func TestCompactionPreservesDeletes(t *testing.T) {
	db := NewTestDB(t, "compact_delete.db")

	db.Put("a", "1")
	db.Delete("a")

	if err := db.Compact(); err != nil {
		t.Fatalf("compaction failed: %v", err)
	}

	_, err := db.Get("a")
	if err == nil {
		t.Fatalf("expected deleted key after compaction")
	}
}

func TestReadAtSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_snapshot.db")
	db, _ := OpenDB(path)
	defer db.Close()
	db.Put("a", "1")
	snap, _ := db.CreateSnapshot()

	db.Put("a", "2")
	snap2, _ := db.CreateSnapshot()

	v1, _ := db.ReadAtSnapshot("a", snap)
	v2, _ := db.ReadAtSnapshot("a", snap2)

	if v1 != "1" {
		t.Fatalf("expected 1, got %s", v1)
	}

	if v2 != "2" {
		t.Fatalf("expected 2, got %s", v2)
	}
}
