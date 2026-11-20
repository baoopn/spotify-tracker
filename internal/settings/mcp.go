package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const keyMCP = "mcp"

// MCP holds the dashboard-managed state of the /mcp endpoint: the admin
// tools toggle and the admin access token. Only the SHA-256 hash of the
// token is stored, never the token itself, so it needs no encryption.
type MCP struct {
	AdminToolsEnabled bool `json:"admin_tools_enabled"`
	// TokenHash is the hex SHA-256 of the token (see HashMCPToken); empty
	// means no dashboard token.
	TokenHash string `json:"token_hash,omitempty"`
	// TokenPrefix is the start of the token, shown in the dashboard.
	TokenPrefix     string     `json:"token_prefix,omitempty"`
	TokenCreatedAt  *time.Time `json:"token_created_at,omitempty"`
	TokenLastUsedAt *time.Time `json:"token_last_used_at,omitempty"`
	UpdatedAt       time.Time  `json:"-"`
}

// DefaultMCP applies when nothing is stored: admin tools on, no token.
var DefaultMCP = MCP{AdminToolsEnabled: true}

// HashMCPToken returns the stored form of an MCP admin token.
func HashMCPToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// MCP returns the MCP settings (ErrNotFound if never set; use DefaultMCP).
func (s *Store) MCP(ctx context.Context) (MCP, error) {
	st, updated, err := load[MCP](ctx, s, keyMCP)
	if err != nil {
		return MCP{}, err
	}
	st.Data.UpdatedAt = updated
	return st.Data, nil
}

// SaveMCP stores the MCP settings.
func (s *Store) SaveMCP(ctx context.Context, actor string, v MCP) error {
	return save(ctx, s, keyMCP, actor, stored[MCP]{Data: v})
}

// TouchMCPToken records that the token with this hash was used at at. It is
// a no-op when the stored token changed meanwhile (rotated or revoked), and
// it leaves updated_by alone so the audit trail of the setting stays intact.
func (s *Store) TouchMCPToken(ctx context.Context, hash string, at time.Time) error {
	if hash == "" {
		return errors.New("touch mcp token: empty hash")
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE settings
		SET value = jsonb_set(value, '{data,token_last_used_at}', to_jsonb($2::timestamptz))
		WHERE key = $3 AND value #>> '{data,token_hash}' = $1`,
		hash, at.UTC(), keyMCP)
	if err != nil {
		return fmt.Errorf("touch mcp token: %w", err)
	}
	return nil
}
