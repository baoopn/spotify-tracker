package settings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHashMCPToken(t *testing.T) {
	// sha256("abc")
	if got := HashMCPToken("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("HashMCPToken(abc) = %s", got)
	}
	if HashMCPToken("stm_a") == HashMCPToken("stm_b") || len(HashMCPToken("")) != 64 {
		t.Error("hash not distinct or not hex SHA-256")
	}
}

func TestDefaultMCP(t *testing.T) {
	if !DefaultMCP.AdminToolsEnabled || DefaultMCP.TokenHash != "" || DefaultMCP.TokenPrefix != "" {
		t.Errorf("DefaultMCP = %+v", DefaultMCP)
	}
}

// The stored JSON has the paths TouchMCPToken's jsonb_set relies on
// ({data,token_hash}, {data,token_last_used_at}) and no UpdatedAt.
func TestMCPStoredShape(t *testing.T) {
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(stored[MCP]{Data: MCP{AdminToolsEnabled: true, TokenHash: "h", TokenPrefix: "stm_abcd",
		TokenCreatedAt: &at, UpdatedAt: at}})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	d := v["data"]
	if d["token_hash"] != "h" || d["admin_tools_enabled"] != true || d["token_prefix"] != "stm_abcd" {
		t.Errorf("stored = %s", raw)
	}
	if strings.Contains(string(raw), "UpdatedAt") || strings.Contains(string(raw), "updated_at") {
		t.Errorf("UpdatedAt stored: %s", raw)
	}
	// Postgres to_jsonb(timestamptz) writes "+00:00" offsets with
	// microseconds; the Go side must read them back.
	var m MCP
	if err := json.Unmarshal([]byte(`{"token_last_used_at":"2026-11-20T12:00:00.123456+00:00"}`), &m); err != nil ||
		m.TokenLastUsedAt == nil || !m.TokenLastUsedAt.Equal(at.Add(123456*time.Microsecond)) {
		t.Errorf("decode Postgres timestamp = %v, %v", m.TokenLastUsedAt, err)
	}
}

// An empty hash is refused before any SQL runs (nil pool: no DB needed).
func TestTouchMCPTokenEmptyHash(t *testing.T) {
	if err := (&Store{}).TouchMCPToken(context.Background(), "", time.Now()); err == nil {
		t.Error("empty hash accepted")
	}
}
