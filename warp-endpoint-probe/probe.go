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
func RunProbes(ctx context.Context, endpoints []Endpoint, concurrency int, perProbeTimeout time.Duration) []ProbeResult {
	if concurrency <= 0 {
		concurrency = 1
	}

	jobs := make(chan Endpoint)
	results := make(chan ProbeResult, len(endpoints))

	var workerGroup sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for endpoint := range jobs {
				latency, err := probeSingleEndpoint(ctx, endpoint, perProbeTimeout)
				if err != nil {
					results <- ProbeResult{Endpoint: endpoint.Address(), Err: err}
					continue
				}
				results <- ProbeResult{Endpoint: endpoint.Address(), Latency: latency}
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

	successful := make([]ProbeResult, 0, len(endpoints))
	for result := range results {
		if result.Err == nil {
			successful = append(successful, result)
		}
	}
	return successful
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
