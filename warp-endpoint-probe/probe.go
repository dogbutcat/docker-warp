package main

import (
	"context"
	"sort"
	"sync"
	"time"
)

// ProbeResult holds the result of a single endpoint probe.
type ProbeResult struct {
	Endpoint string
	Latency  time.Duration
	Err      error
}

// SortProbeResults sorts successful probe results by latency ascending.
func SortProbeResults(results []ProbeResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})
}

// RunProbes executes probes with bounded concurrency.
// rounds 指定每个目标被测试的次数，取平均延时。
func RunProbes(ctx context.Context, endpoints []Endpoint, concurrency int, perProbeTimeout time.Duration, rounds int) []ProbeResult {
	if concurrency <= 0 {
		concurrency = 1
	}
	if rounds <= 0 {
		rounds = 1
	}

	jobs := make(chan Endpoint)
	results := make(chan ProbeResult, len(endpoints))

	var workerGroup sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for endpoint := range jobs {
				r := probeWithRounds(ctx, endpoint, perProbeTimeout, rounds)
				results <- r
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, endpoint := range endpoints {
			select {
			case <-ctx.Done():
				return
			case jobs <- endpoint:
			}
		}
	}()

	go func() {
		workerGroup.Wait()
		close(results)
	}()

	// 关键修复：按 Latency > 0 过滤（而非 Err == nil），
	// 因为 MASQUE 节点会先回应 ServerHello 再拒绝证书，
	// 此时 err != nil 但 RTT 仍然有效。
	successful := make([]ProbeResult, 0, len(endpoints))
	for result := range results {
		if result.Latency > 0 {
			successful = append(successful, result)
		}
	}
	return successful
}

// probeWithRounds 对同一个 endpoint 进行 rounds 轮探测，返回平均延时。
func probeWithRounds(ctx context.Context, endpoint Endpoint, timeout time.Duration, rounds int) ProbeResult {
	var totalLatency time.Duration
	var responded int
	var lastErr error

	for i := 0; i < rounds; i++ {
		select {
		case <-ctx.Done():
			return ProbeResult{Endpoint: endpoint.Address(), Err: ctx.Err()}
		default:
		}

		latency, err := probeSingleEndpoint(ctx, endpoint, timeout)
		if err != nil {
			lastErr = err
		}
		if latency > 0 {
			responded++
			totalLatency += latency
		}
		// 轮间间隔，避免触发 rate-limit
		if i < rounds-1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	r := ProbeResult{
		Endpoint: endpoint.Address(),
		Err:      lastErr,
	}
	if responded > 0 {
		r.Latency = totalLatency / time.Duration(responded)
	}
	return r
}

func probeSingleEndpoint(ctx context.Context, endpoint Endpoint, timeout time.Duration) (time.Duration, error) {
	switch endpoint.Probe {
	case ProbeWireGuard:
		return ProbeWireGuardHandshake(ctx, endpoint, timeout)
	case ProbeQUIC:
		return ProbeQUICHandshake(ctx, endpoint, timeout)
	case ProbeHTTPS:
		return ProbeHTTPSHandshake(ctx, endpoint, timeout)
	default:
		return 0, ErrUnsupportedProbe
	}
}
