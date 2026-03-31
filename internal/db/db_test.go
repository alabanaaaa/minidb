package db

import (
	"testing"
)

func TestNewInvalidURL(t *testing.T) {
	_, err := New("postgres://invalid:invalid@localhost:99999/baddb")
	if err == nil {
		t.Fatal("expected error for invalid database URL")
	}
}

func TestNewEmptyURL(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Fatal("expected error for empty database URL")
	}
}
