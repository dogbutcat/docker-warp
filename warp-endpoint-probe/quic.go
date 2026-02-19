package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/quic-go/quic-go"
)

// ProbeQUICHandshake performs a QUIC handshake to measure RTT.
func ProbeQUICHandshake(ctx context.Context, endpoint Endpoint, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	serverName := endpoint.SNI
	if serverName == "" {
		serverName = DefaultSNI
	}

	tlsConf := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}

	quicConf := &quic.Config{
		HandshakeIdleTimeout: timeout,
	}

	start := time.Now()
	conn, err := quic.DialAddr(probeCtx, endpoint.Address(), tlsConf, quicConf)
	latency := time.Since(start)

	if err != nil {
		return 0, fmt.Errorf("quic handshake %s: %w", endpoint.Address(), err)
	}
	_ = conn.CloseWithError(0, "probe")

	return latency, nil
}
