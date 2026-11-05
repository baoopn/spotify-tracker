package secrets

import (
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	key, err := ParseKey(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal("refresh-token-123")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "refresh-token") || !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("not encrypted: %s", sealed)
	}
	other, _ := b.Seal("refresh-token-123")
	if other == sealed {
		t.Error("nonce reuse: identical ciphertexts")
	}
	got, err := b.Open(sealed)
	if err != nil || got != "refresh-token-123" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if s, _ := b.Seal(""); s != "" {
		t.Error("empty should stay empty")
	}

	wrong, _ := New([]byte(strings.Repeat("x", 32)))
	if _, err := wrong.Open(sealed); err == nil {
		t.Error("expected failure with wrong key")
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey("short"); err == nil {
		t.Error("expected error")
	}
	if _, err := ParseKey("q83vEjRWeJCrze8SNFZ4kKvN7xI0VniQq83vEjRWeJA="); err != nil {
		t.Errorf("base64 key rejected: %v", err)
	}
}
