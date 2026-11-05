// Package ops provides the operational HTTP endpoints every service exposes
// (/healthz, /readyz, /metrics) plus the container healthcheck probe.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Check reports whether a dependency is ready. It returns nil when healthy.
type Check func(ctx context.Context) error

// Server is the ops HTTP server.
type Server struct {
	srv    *http.Server
	checks map[string]Check
	log    *slog.Logger
}

// NewServer creates an ops server on addr.
func NewServer(addr string, log *slog.Logger) *Server {
	s := &Server{checks: map[string]Check{}, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", s.ready)
	mux.Handle("GET /metrics", promhttp.Handler())
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// AddCheck registers a named readiness check.
func (s *Server) AddCheck(name string, c Check) { s.checks[name] = c }

// Handler returns the ops handler (useful to mount on another server).
func (s *Server) Handler() http.Handler { return s.srv.Handler }

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result := map[string]string{}
	status := http.StatusOK
	for name, c := range s.checks {
		if err := c(ctx); err != nil {
			result[name] = err.Error()
			status = http.StatusServiceUnavailable
		} else {
			result[name] = "ok"
		}
	}
	writeJSON(w, status, map[string]any{"checks": result})
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	return RunHTTP(ctx, s.srv, s.log)
}

// RunHTTP runs srv until ctx is done and then shuts it down gracefully.
func RunHTTP(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", srv.Addr, err)
	}
	log.Info("http server listening", slog.String("addr", ln.Addr().String()))
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// Probe performs a container healthcheck against the local ops server and
// returns the process exit code. It is used as `<binary> healthcheck` so that
// distroless images need no curl/wget.
func Probe(addr string) int {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/healthz", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck failed: status", resp.StatusCode)
		return 1
	}
	return 0
}

// IsHealthcheck reports whether the process was invoked as a healthcheck.
func IsHealthcheck() bool {
	return len(os.Args) > 1 && (os.Args[1] == "healthcheck" || os.Args[1] == "--health")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
