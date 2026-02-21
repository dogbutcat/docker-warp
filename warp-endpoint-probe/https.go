package main

import (
	"context"
	"fmt"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
)

// ProbeHTTPSHandshake measures dial + TLS handshake latency on TCP/443.
// Uses uTLS with Chrome fingerprint to bypass DPI/GFW SNI detection.
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

	// 1. 建立底层 TCP 连接
	start := time.Now()
	tcpConn, err := dialer.DialContext(probeCtx, "tcp", endpoint.Address())
	if err != nil {
		return 0, fmt.Errorf("tcp dial %s: %w", endpoint.Address(), err)
	}

	// 2. 构造 uTLS 配置并注入目标 SNI
	tlsConfig := &utls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
	}

	// 3. 包装 TCP 连接，贴上 Chrome 浏览器的 TLS 指纹特征
	uConn := utls.UClient(tcpConn, tlsConfig, utls.HelloChrome_Auto)

	// 4. 触发伪装握手
	if err := uConn.HandshakeContext(probeCtx); err != nil {
		latency := time.Since(start)
		_ = tcpConn.Close()
		// TCP 已连通但 TLS 握手失败 → 服务端有回应，RTT 仍然有效
		if latency < timeout-50*time.Millisecond {
			return latency, err
		}
		return 0, fmt.Errorf("utls handshake %s: %w", endpoint.Address(), err)
	}
	latency := time.Since(start)
	_ = uConn.Close()

	return latency, nil
}
