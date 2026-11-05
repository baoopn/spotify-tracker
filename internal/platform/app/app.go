// Package app holds process bootstrap helpers shared by every binary.
package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/baoopn/spotify-tracker/internal/platform/ops"
)

// Version is set at build time via -ldflags "-X .../app.Version=...".
var Version = "dev"

// Main runs fn with a context cancelled on SIGINT/SIGTERM and exits with a
// non-zero status on error. When invoked as `<binary> healthcheck` it probes
// the local ops server instead (for distroless container healthchecks).
func Main(fn func(ctx context.Context) error) {
	if ops.IsHealthcheck() {
		addr := os.Getenv("OPS_ADDR")
		if addr == "" {
			addr = ":9090"
		}
		os.Exit(ops.Probe(addr))
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println(Version)
		return
	}

	if err := run(fn); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run(fn func(ctx context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return fn(ctx)
}
