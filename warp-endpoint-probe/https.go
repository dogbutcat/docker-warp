package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// ProbeHTTPSHandshake measures dial + TLS handshake latency on TCP/443.
func ProbeHTTPSHandshake(ctx context.Context, endpoint Endpoint, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	serverName := endpoint.SNI
	if serverName == "" {
		serverName = DefaultSNI
	}

	dialer := &net.Dialer{Timeout: timeout}
	tlsConfig := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
	}

	start := time.Now()
	conn, err := tls.DialWithDialer(dialer, "tcp", endpoint.Address(), tlsConfig)
	if err != nil {
		return 0, fmt.Errorf("https handshake %s: %w", endpoint.Address(), err)
	}
	latency := time.Since(start)
	_ = conn.Close()

	select {
	case <-probeCtx.Done():
		if probeCtx.Err() != nil {
			return 0, probeCtx.Err()
		}
	default:
	}

	return latency, nil
}
