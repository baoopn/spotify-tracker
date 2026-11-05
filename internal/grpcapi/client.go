package grpcapi

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// tokenCreds attaches the internal bearer token to every call.
type tokenCreds string

func (t tokenCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(t)}, nil
}

// RequireTransportSecurity is false: traffic stays on the private Docker
// network between containers.
func (tokenCreds) RequireTransportSecurity() bool { return false }

// Dial creates a client connection to the tracker API's gRPC endpoint.
func Dial(addr, token string) (*grpc.ClientConn, error) {
	const retryPolicy = `{
		"methodConfig": [{
			"name": [{"service": "tracker.v1.IngestService"}, {"service": "tracker.v1.EnrichmentService"}, {"service": "tracker.v1.SpotifyService"}],
			"timeout": "10s",
			"retryPolicy": {
				"maxAttempts": 4,
				"initialBackoff": "0.2s",
				"maxBackoff": "2s",
				"backoffMultiplier": 2,
				"retryableStatusCodes": ["UNAVAILABLE"]
			}
		}]
	}`
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokenCreds(token)),
		grpc.WithDefaultServiceConfig(retryPolicy),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", addr, err)
	}
	return conn, nil
}
