package integrations

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	s := &Service{tokenEncryptionKey: deriveTokenKey("super-secret")}
	enc, err := s.encryptToken("token-value")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if enc == "token-value" {
		t.Fatal("expected encrypted token to differ from plaintext")
	}
	dec, err := s.decryptToken(enc)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if dec != "token-value" {
		t.Fatalf("expected decrypted token to match original, got %q", dec)
	}
}

func TestEncryptWithoutKeyFallsBackToPlaintext(t *testing.T) {
	s := &Service{}
	enc, err := s.encryptToken("token-value")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if enc != "token-value" {
		t.Fatalf("expected plaintext fallback, got %q", enc)
	}
	dec, err := s.decryptToken(enc)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if dec != "token-value" {
		t.Fatalf("expected same token, got %q", dec)
	}
}

func TestTokenExpired(t *testing.T) {
	if tokenExpired("") {
		t.Fatal("empty expiry must not be expired")
	}
	if !tokenExpired("2000-01-01T00:00:00Z") {
		t.Fatal("old timestamp should be expired")
	}
}
