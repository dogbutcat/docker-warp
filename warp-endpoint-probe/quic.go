package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
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

	if conn != nil {
		_ = conn.CloseWithError(0, "probe")
	}

	// 即使握手因缺客户端证书被拒绝 (CRYPTO_ERROR 0x128)，
	// 只要服务端回应了 ServerHello，latency < timeout 即为有效 RTT。
	if err != nil {
		errStr := err.Error()
		// 如果是 Connection Refused 等网络错误，立马失败，防止把死节点当优选
		if !strings.Contains(errStr, "CRYPTO_ERROR") && !strings.Contains(errStr, "APPLICATION_ERROR") {
			return 0, fmt.Errorf("quic handshake %s: %w", endpoint.Address(), err)
		}

		// 真正超时（完全没回应）时返回 0
		if latency >= timeout-50*time.Millisecond {
			return 0, fmt.Errorf("quic handshake timeout %s: %w", endpoint.Address(), err)
		}
	}
	return latency, err
}
