package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

func main() {
	mode := flag.String("mode", "tunnel", "Probe mode: tunnel | api")
	target := flag.String("target", "", "Tunnel target: consumer | wireguard | masque (optional)")
	ipv6 := flag.Bool("6", false, "Include IPv6 targets")
	concurrency := flag.Int("n", runtime.NumCPU()*2, "Number of concurrent goroutines")
	totalTimeoutStr := flag.String("timeout", "3s", "Hard timeout for all probes")
	outputFile := flag.String("o", "result.csv", "Output CSV file path")
	flag.Parse()

	if *concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "ERROR: -n must be > 0")
		os.Exit(2)
	}

	totalTimeout, err := time.ParseDuration(*totalTimeoutStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: invalid timeout %q: %v\n", *totalTimeoutStr, err)
		os.Exit(2)
	}

	pool, err := SelectPool(*mode, *target, os.Getenv("WARP_TUNNEL_PROTOCOL"), isEnvTrue("WARP_MDM_ENABLED"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: selecting target pool: %v\n", err)
		os.Exit(2)
	}

	endpoints, err := ExpandTargets(pool, *ipv6)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: expanding targets: %v\n", err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "Mode=%s Pool=%s Targets=%d\n", *mode, pool.Name, len(endpoints))

	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	results := RunProbes(ctx, endpoints, *concurrency, time.Second)
	SortProbeResults(results)

	if err := writeCSV(*outputFile, results); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: writing CSV: %v\n", err)
		os.Exit(1)
	}

	if len(results) > 0 {
		fmt.Fprintf(os.Stderr, "Best: %s (%.1fms)\n",
			results[0].Endpoint, float64(results[0].Latency)/float64(time.Millisecond))
	} else {
		fmt.Fprintln(os.Stderr, "No reachable endpoints found")
		os.Exit(1)
	}
}

func writeCSV(path string, results []ProbeResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	for _, r := range results {
		latencyMs := fmt.Sprintf("%d", r.Latency.Milliseconds())
		if err := w.Write([]string{r.Endpoint, latencyMs}); err != nil {
			return err
		}
	}
	return nil
}

func inferTunnelTarget(protocol string, mdm bool) string {
	if !mdm {
		return "consumer"
	}
	if protocol == "masque" {
		return "masque"
	}
	return "wireguard"
}

func isEnvTrue(name string) bool {
	value := os.Getenv(name)
	return value == "true" || value == "1" || value == "yes"
}
