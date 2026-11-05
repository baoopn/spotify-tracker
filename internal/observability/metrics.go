// Package observability defines Prometheus metrics shared across services.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "spotify_tracker"

var (
	// HTTPRequests counts public HTTP requests.
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "http_requests_total",
		Help: "HTTP requests by route and status code.",
	}, []string{"route", "method", "code"})

	// HTTPDuration observes public HTTP latency.
	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "http_request_duration_seconds",
		Help:    "HTTP request latency by route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	// GRPCRequests counts gRPC calls handled by the API.
	GRPCRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "grpc_requests_total",
		Help: "gRPC requests by method and status code.",
	}, []string{"method", "code"})

	// HistoryEntries counts listening history rows created.
	HistoryEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace, Name: "history_entries_created_total",
		Help: "Listening history entries created.",
	})

	// Replays counts replay increments.
	Replays = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace, Name: "replays_total",
		Help: "Replay counter increments.",
	})

	// EnrichmentResults counts enrichment outcomes by status.
	EnrichmentResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "enrichment_results_total",
		Help: "Enrichment outcomes by status.",
	}, []string{"status"})

	// PollCycles counts poller cycles by outcome.
	PollCycles = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "poll_cycles_total",
		Help: "Poller cycles by outcome (playing, idle, error, rate_limited).",
	}, []string{"outcome"})

	// UpstreamRequests counts calls to third-party APIs.
	UpstreamRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "upstream_requests_total",
		Help: "Requests to upstream APIs by provider and status code.",
	}, []string{"provider", "code"})
)
