package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// extractIP extracts the IP portion from an "IP:port" or "[IPv6]:port" string.
func extractIP(endpoint string) string {
	if strings.HasPrefix(endpoint, "[") {
		// IPv6: [::1]:443
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil {
			return endpoint
		}
		return host
	}
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return endpoint
	}
	return host
}

// VerifyICMP runs `ping` against the IP extracted from the endpoint.
// Returns nil if the host responds within the given timeout.
func VerifyICMP(endpoint string, timeout time.Duration) error {
	ip := extractIP(endpoint)
	if ip == "" {
		return fmt.Errorf("empty ip from endpoint %s", endpoint)
	}

	timeoutSec := fmt.Sprintf("%.0f", timeout.Seconds())
	if timeout.Seconds() < 1 {
		timeoutSec = "1"
	}

	// -c 1: send 1 packet, -W: timeout in seconds
	cmd := exec.Command("ping", "-c", "1", "-W", timeoutSec, ip)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("icmp ping %s failed: %s", ip, strings.TrimSpace(string(output)))
	}
	return nil
}

// FilterByICMP verifies sorted probe results with ICMP ping.
// Returns the first result that passes ICMP verification within topN candidates.
// If none pass, returns all original results unchanged (soft-fail).
func FilterByICMP(results []ProbeResult, topN int, timeout time.Duration) []ProbeResult {
	if len(results) == 0 {
		return results
	}
	if topN <= 0 || topN > len(results) {
		topN = len(results)
	}

	for i := 0; i < topN; i++ {
		if err := VerifyICMP(results[i].Endpoint, timeout); err != nil {
			fmt.Fprintf(os.Stderr, "ICMP fail: %s (%v)\n", results[i].Endpoint, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "ICMP pass: %s\n", results[i].Endpoint)
		// Move the verified result to the front
		verified := results[i]
		return append([]ProbeResult{verified}, append(results[:i], results[i+1:]...)...)
	}

	fmt.Fprintln(os.Stderr, "WARN: no endpoint passed ICMP, using latency-sorted results")
	return results
}
