package auth

import (
	"testing"
	"time"
)

const testSecret = "test-secret-key-for-unit-tests"

func TestGenerateAndVerifyToken(t *testing.T) {
	svc := NewService(testSecret)

	access, refresh, err := svc.GenerateToken("user-1", "shop-1", "test@example.com", "owner")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if access == "" {
		t.Fatal("access token is empty")
	}
	if refresh == "" {
		t.Fatal("refresh token is empty")
	}

	claims, err := svc.VerifyToken(access)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if claims.UserID != "user-1" {
		t.Errorf("expected user-1, got %s", claims.UserID)
	}
	if claims.ShopID != "shop-1" {
		t.Errorf("expected shop-1, got %s", claims.ShopID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", claims.Email)
	}
	if claims.Role != "owner" {
		t.Errorf("expected owner, got %s", claims.Role)
	}
}

func TestVerifyInvalidToken(t *testing.T) {
	svc := NewService(testSecret)

	_, err := svc.VerifyToken("not-a-real-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestVerifyTokenWrongSecret(t *testing.T) {
	svc1 := NewService("secret-1")
	svc2 := NewService("secret-2")

	token, _, err := svc1.GenerateToken("user-1", "shop-1", "test@example.com", "owner")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = svc2.VerifyToken(token)
	if err == nil {
		t.Fatal("expected error when verifying with wrong secret")
	}
}

func TestTokenExpiration(t *testing.T) {
	svc := NewService(testSecret)
	svc.tokenTTL = -1 * time.Second

	token, _, err := svc.GenerateToken("user-1", "shop-1", "test@example.com", "owner")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, err = svc.VerifyToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestHashPassword(t *testing.T) {
	password := "secure-password-123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if hash == password {
		t.Fatal("hash should not equal plaintext")
	}
	if len(hash) < 20 {
		t.Fatalf("hash too short: %d", len(hash))
	}
}

func TestCheckPassword(t *testing.T) {
	password := "secure-password-123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	err = CheckPassword(hash, password)
	if err != nil {
		t.Fatalf("CheckPassword failed for correct password: %v", err)
	}

	err = CheckPassword(hash, "wrong-password")
	if err == nil {
		t.Fatal("CheckPassword should fail for wrong password")
	}
}

func TestDifferentPasswordsProduceDifferentHashes(t *testing.T) {
	hash1, _ := HashPassword("password1")
	hash2, _ := HashPassword("password2")

	if hash1 == hash2 {
		t.Fatal("different passwords should produce different hashes")
	}
}

func TestSamePasswordProducesDifferentHashes(t *testing.T) {
	hash1, _ := HashPassword("same-password")
	hash2, _ := HashPassword("same-password")

	if hash1 == hash2 {
		t.Fatal("bcrypt should produce different salts for same password")
	}
}
