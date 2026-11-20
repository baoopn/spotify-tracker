package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/mcpserver"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// MCPInfo describes the /mcp endpoint for GET /mcp.
type MCPInfo struct {
	// Enabled mirrors MCP_ENABLED.
	Enabled bool
	// EndpointURL is PUBLIC_URL + "/mcp".
	EndpointURL string
	// EnvTokenConfigured reports whether MCP_ADMIN_TOKEN is set.
	EnvTokenConfigured bool
	// RateLimitPerMinute mirrors MCP_RATE_LIMIT_PER_MINUTE (0 = off).
	RateLimitPerMinute int
}

// mcpSettingsStore loads and saves the MCP settings (implemented by
// *settings.Store).
type mcpSettingsStore interface {
	MCP(ctx context.Context) (settings.MCP, error)
	SaveMCP(ctx context.Context, actor string, v settings.MCP) error
}

const (
	// mcpTokenPrefix starts every dashboard-generated MCP token.
	mcpTokenPrefix = "stm_"
	// mcpTokenBytes is the random part of a token.
	mcpTokenBytes = 32
	// mcpShownPrefix is how much of a token the dashboard shows.
	mcpShownPrefix = 8
)

// mountMCP registers the MCP settings routes. They are writes or reveal the
// token state, so they are never part of ReadOnlyHandler: an MCP client
// cannot mint its own tokens.
func (a *API) mountMCP(r chi.Router) {
	r.Get("/mcp", a.getMCP)
	r.Put("/mcp", a.putMCP)
	r.Post("/mcp/token", a.generateMCPToken)
	r.Delete("/mcp/token", a.revokeMCPToken)
}

func (a *API) mcpDB() mcpSettingsStore {
	if a.mcpSettings != nil {
		return a.mcpSettings
	}
	return a.Settings
}

func (a *API) loadMCP(ctx context.Context) (settings.MCP, error) {
	v, err := a.mcpDB().MCP(ctx)
	if errors.Is(err, settings.ErrNotFound) {
		return settings.DefaultMCP, nil
	}
	return v, err
}

// saveMCP stores v and drops the MCP server's cached copy.
func (a *API) saveMCP(r *http.Request, v settings.MCP) error {
	u, _ := UserFrom(r.Context())
	if err := a.mcpDB().SaveMCP(r.Context(), u.Email, v); err != nil {
		return err
	}
	if a.OnMCPChange != nil {
		a.OnMCPChange()
	}
	return nil
}

type mcpTokenView struct {
	Configured bool       `json:"configured"`
	Prefix     string     `json:"prefix"`
	CreatedAt  *time.Time `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type mcpView struct {
	Enabled            bool                 `json:"enabled"`
	EndpointURL        string               `json:"endpoint_url"`
	AdminToolsEnabled  bool                 `json:"admin_tools_enabled"`
	Token              *mcpTokenView        `json:"token"`
	EnvTokenConfigured bool                 `json:"env_token_configured"`
	RateLimitPerMinute int                  `json:"rate_limit_per_minute"`
	PublicTools        []mcpserver.ToolInfo `json:"public_tools"`
	AdminTools         []mcpserver.ToolInfo `json:"admin_tools"`
}

func (a *API) toMCPView(s settings.MCP) mcpView {
	v := mcpView{
		Enabled: a.MCP.Enabled, EndpointURL: a.MCP.EndpointURL, AdminToolsEnabled: s.AdminToolsEnabled,
		EnvTokenConfigured: a.MCP.EnvTokenConfigured, RateLimitPerMinute: a.MCP.RateLimitPerMinute,
		PublicTools: mcpserver.PublicTools(), AdminTools: mcpserver.AdminTools(),
	}
	if s.TokenHash != "" {
		v.Token = &mcpTokenView{Configured: true, Prefix: s.TokenPrefix,
			CreatedAt: optUTC(s.TokenCreatedAt), LastUsedAt: optUTC(s.TokenLastUsedAt)}
	}
	return v
}

func (a *API) getMCP(w http.ResponseWriter, r *http.Request) {
	s, err := a.loadMCP(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.toMCPView(s))
}

func (a *API) putMCP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AdminToolsEnabled *bool `json:"admin_tools_enabled"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	if in.AdminToolsEnabled == nil {
		writeValidation(w, map[string]string{"admin_tools_enabled": "is required"})
		return
	}
	s, err := a.loadMCP(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	s.AdminToolsEnabled = *in.AdminToolsEnabled
	if err := a.saveMCP(r, s); err != nil {
		a.internal(w, r, err)
		return
	}
	a.audit(r, "mcp.settings_updated", map[string]any{"admin_tools_enabled": s.AdminToolsEnabled})
	writeJSON(w, http.StatusOK, a.toMCPView(s))
}

// newMCPToken returns a fresh token: "stm_" + 32 random bytes, base64url.
func newMCPToken() (string, error) {
	b := make([]byte, mcpTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return mcpTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// generateMCPToken creates or rotates the dashboard token. The token is in
// this response only; the settings keep its hash.
func (a *API) generateMCPToken(w http.ResponseWriter, r *http.Request) {
	s, err := a.loadMCP(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	tok, err := newMCPToken()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	rotated := s.TokenHash != ""
	now := a.clock().UTC().Truncate(time.Second)
	s.TokenHash = settings.HashMCPToken(tok)
	s.TokenPrefix = tok[:mcpShownPrefix]
	s.TokenCreatedAt, s.TokenLastUsedAt = &now, nil
	if err := a.saveMCP(r, s); err != nil {
		a.internal(w, r, err)
		return
	}
	a.audit(r, "mcp.token_generated", map[string]any{"prefix": s.TokenPrefix, "rotated": rotated})
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "prefix": s.TokenPrefix, "created_at": now})
}

// revokeMCPToken removes the dashboard token. Revoking when there is none
// is not an error.
func (a *API) revokeMCPToken(w http.ResponseWriter, r *http.Request) {
	s, err := a.loadMCP(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if s.TokenHash != "" {
		prefix := s.TokenPrefix
		s.TokenHash, s.TokenPrefix, s.TokenCreatedAt, s.TokenLastUsedAt = "", "", nil, nil
		if err := a.saveMCP(r, s); err != nil {
			a.internal(w, r, err)
			return
		}
		a.audit(r, "mcp.token_revoked", map[string]any{"prefix": prefix})
	}
	writeJSON(w, http.StatusOK, a.toMCPView(s))
}
