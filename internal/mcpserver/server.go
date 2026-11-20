// Package mcpserver is the remote MCP endpoint (/mcp) of cmd/api: a
// stateless Streamable HTTP server whose tools answer from the public
// /api/v1 routes and, for callers with the admin token, from a read-only
// subset of the admin API. Tools run in-process against those handlers, so
// there is no duplicated query code and the Redis cache is shared.
package mcpserver

import (
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxRequestBody caps the JSON-RPC request body.
const maxRequestBody = 1 << 20

// Options configures the MCP endpoint.
type Options struct {
	// Public serves the read-only /api/v1 routes (httpapi.Handler.V1Handler).
	Public http.Handler
	// Admin serves the read-only admin routes (admin.ReadOnlyHandler); nil
	// disables the admin tools.
	Admin http.Handler
	// AdminToken (MCP_ADMIN_TOKEN) is a bootstrap admin token; empty means
	// none.
	AdminToken string
	// Settings supplies the dashboard-managed token and the admin tools
	// toggle; nil means the env token only, with the admin tools on.
	Settings *SettingsCache
	// Version is reported as the server version ("dev" when empty).
	Version string
	Log     *slog.Logger
}

type serverKey struct{}

// New returns the /mcp handler. Requests without a bearer token get the
// public tools. A valid admin token (the env token, or the dashboard token
// whose hash is stored in the settings) adds the admin tools while they are
// enabled in the settings; any other token is rejected with 401 (a wrong
// token is never downgraded to anonymous).
func New(opts Options) http.Handler {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	impl := &mcp.Implementation{Name: "spotify-tracker", Title: "Spotify listening tracker", Version: version}

	public := mcp.NewServer(impl, &mcp.ServerOptions{Logger: log})
	addTools(public, publicTools, opts.Public, "/api/v1", log, true)

	var admin *mcp.Server
	if opts.Admin != nil {
		admin = mcp.NewServer(impl, &mcp.ServerOptions{Logger: log})
		addTools(admin, publicTools, opts.Public, "/api/v1", log, true)
		addTools(admin, adminTools, opts.Admin, "", log, false)
	}

	sdk := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if s, ok := r.Context().Value(serverKey{}).(*mcp.Server); ok {
			return s
		}
		return public
	}, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		Logger:              log,
		MaxRequestBodyBytes: maxRequestBody,
	})

	a := &authorizer{envToken: []byte(opts.AdminToken), settings: opts.Settings, admin: admin, log: log}
	return a.middleware(sdk)
}
