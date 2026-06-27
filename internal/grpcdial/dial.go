// Package grpcdial owns the shared dial-option construction the BFF's two
// upstream clients (price-service, indexer-service) reuse. Centralizing the
// keepalive / TLS / timeout policy keeps the two clients consistent.
package grpcdial

import (
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/asolovov/evm-oracle-demo-api/config"
)

// Options renders config.GRPCClientConfig into a list of grpc.DialOption.
func Options(cfg config.GRPCClientConfig) ([]grpc.DialOption, error) {
	keepAliveTime, err := time.ParseDuration(cfg.KeepAlive.Time)
	if err != nil {
		return nil, fmt.Errorf("parse grpc_client.keep_alive.time: %w", err)
	}
	keepAliveTimeout, err := time.ParseDuration(cfg.KeepAlive.Timeout)
	if err != nil {
		return nil, fmt.Errorf("parse grpc_client.keep_alive.timeout: %w", err)
	}

	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                keepAliveTime,
			Timeout:             keepAliveTimeout,
			PermitWithoutStream: cfg.KeepAlive.PermitWithoutStream,
		}),
	}

	if cfg.UseTLS {
		// System CA trust — every upstream this BFF talks to terminates
		// TLS at the in-cluster ingress and presents a server cert chain
		// rooted at a well-known CA. Bring-your-own-CA can be added later
		// as a config block; for the demo this is enough.
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
		})))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	return opts, nil
}
