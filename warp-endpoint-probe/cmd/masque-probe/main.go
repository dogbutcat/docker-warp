// masque-probe: 极简版 MASQUE (HTTP/3 over QUIC) 握手延时探针
// 从指定 CIDR 中每段取样 IP，发起 QUIC ClientHello (ALPN=h3)，
// 无论握手成功或失败（因无客户端证书）均记录 RTT。
// 支持多轮探测计算平均延时，消除偶发性网络抖动。
package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// --- CIDR 与目标配置 ---

// jdcloud API 端点 CIDR（来自 targets.go apiTargets）
var apiCIDRs = []string{
	"14.204.96.224/27", "27.36.126.224/27", "27.128.218.224/27",
	"36.136.95.32/27", "36.147.52.160/27", "36.154.11.224/27",
	"42.236.121.160/27", "60.13.99.64/26", "101.69.205.224/27",
	"103.44.252.32/27", "106.225.240.96/27", "111.7.87.160/27",
	"111.48.87.160/27", "111.170.27.96/27", "111.177.11.224/27",
	"112.49.47.96/27", "113.56.217.96/27", "114.67.161.32/27",
	"114.67.192.208/28", "116.163.41.64/26", "116.198.49.144/28",
	"116.198.165.16/28", "117.187.40.32/27", "117.187.185.32/27",
	"119.0.67.32/27", "119.6.235.32/27", "119.188.204.32/27",
	"120.206.188.224/27", "120.220.55.96/27", "120.226.37.160/27",
	"121.17.125.32/27", "122.190.152.160/27", "122.226.163.224/27",
	"123.138.203.160/27", "124.166.232.32/27", "124.225.84.32/27",
	"124.236.72.32/27", "125.77.31.224/27", "150.138.153.192/26",
	"182.201.240.224/27", "183.131.87.224/27", "198.41.130.16/28",
	"218.60.77.224/27", "218.205.95.64/27", "218.207.1.32/27",
	"220.185.189.128/25", "222.211.66.64/27", "223.85.111.224/27",
}

// masque 隧道 CIDR（来自 targets.go tunnelTargets["masque"]）
var masqueCIDRs = []string{
	"162.159.197.0/24",
}

// --- 结果结构 ---

// ProbeResult 保存单个 IP 的多轮探测汇总结果。
type ProbeResult struct {
	Addr        string
	AvgLatency  time.Duration // 有效轮次的平均延时
	MinLatency  time.Duration // 最低延时
	MaxLatency  time.Duration // 最高延时
	Rounds      int           // 总轮次数
	Responded   int           // 收到回应的轮次数（latency > 0）
	CryptoErr   int           // CRYPTO_ERROR 轮次数
	ConnRefused int           // Connection Refused 轮次数
	TimeoutErr  int           // 超时（完全无回应）轮次数
	OtherErr    int           // 其他错误轮次数
	LastErr     string        // 最后一次错误信息（如有）
}

// --- CIDR 展开（每个段取样 samplePerCIDR 个 IP）---

func sampleFromCIDR(cidr string, count int) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("only IPv4 supported: %s", cidr)
	}

	ones, bits := ipNet.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 1 {
		return nil, nil
	}

	base := binary.BigEndian.Uint32(ipv4)
	hostCount := int(uint32(1<<hostBits) - 2)
	if count > hostCount {
		count = hostCount
	}

	// 均匀采样
	step := hostCount / count
	if step < 1 {
		step = 1
	}

	hosts := make([]string, 0, count)
	for i := 1; len(hosts) < count && i <= hostCount; i += step {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], base+uint32(i))
		hosts = append(hosts, net.IPv4(buf[0], buf[1], buf[2], buf[3]).String())
	}
	return hosts, nil
}

// --- QUIC 握手探针（不管成功失败都测 RTT）---

func probeQUIC(ctx context.Context, addr string, sni string, timeout time.Duration) (time.Duration, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tlsConf := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}

	quicConf := &quic.Config{
		HandshakeIdleTimeout: timeout,
	}

	start := time.Now()
	conn, err := quic.DialAddr(probeCtx, addr, tlsConf, quicConf)
	latency := time.Since(start)

	if conn != nil {
		_ = conn.CloseWithError(0, "probe")
	}

	// 即使握手报错（如缺少客户端证书被拒绝），
	// 如果延时远小于超时值，说明服务端确实回应了 ServerHello，
	// 此时 latency 仍然有效。
	if err != nil && latency >= timeout-50*time.Millisecond {
		// 真正的超时 = 完全没回应
		return 0, err
	}
	return latency, err
}

// probeMultiRound 对同一个 addr 进行 rounds 轮探测，返回汇总结果。
func probeMultiRound(ctx context.Context, addr, sni string, timeout time.Duration, rounds int) ProbeResult {
	r := ProbeResult{
		Addr:   addr,
		Rounds: rounds,
	}

	var totalLatency time.Duration
	var lastErr string

	for i := 0; i < rounds; i++ {
		lat, err := probeQUIC(ctx, addr, sni, timeout)
		if err != nil {
			lastErr = err.Error()
			errStr := err.Error()
			switch {
			case strings.Contains(errStr, "CRYPTO_ERROR") || strings.Contains(errStr, "APPLICATION_ERROR"):
				r.CryptoErr++
			case strings.Contains(errStr, "connection refused"):
				r.ConnRefused++
			case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") || lat == 0:
				r.TimeoutErr++
			default:
				r.OtherErr++
			}
		}
		if lat > 0 {
			r.Responded++
			totalLatency += lat
			if r.MinLatency == 0 || lat < r.MinLatency {
				r.MinLatency = lat
			}
			if lat > r.MaxLatency {
				r.MaxLatency = lat
			}
		}
		// 轮间短暂间隔，避免被 rate-limit
		if i < rounds-1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if r.Responded > 0 {
		r.AvgLatency = totalLatency / time.Duration(r.Responded)
	}
	r.LastErr = lastErr
	return r
}

// --- 主函数 ---

func main() {
	mode := flag.String("mode", "masque", "masque | api (ignored when -cidr is set)")
	cidrFlag := flag.String("cidr", "", "Custom CIDRs to probe (comma-separated, e.g. '1.2.3.0/24,5.6.7.0/27')")
	port := flag.Int("port", 443, "Target UDP port")
	sni := flag.String("sni", "zero-trust-client.cloudflareclient.com", "TLS SNI")
	sampleN := flag.Int("sample", 3, "IPs to sample per CIDR")
	rounds := flag.Int("rounds", 3, "Probe rounds per IP (average over N rounds)")
	concurrency := flag.Int("n", 20, "Concurrent probes")
	timeoutStr := flag.String("timeout", "2s", "Per-probe timeout")
	topN := flag.Int("top", 20, "Show top N fastest results")
	flag.Parse()

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad timeout: %v\n", err)
		os.Exit(1)
	}

	// 选 CIDR 池：-cidr 优先，否则按 -mode 选内置列表
	var cidrs []string
	if *cidrFlag != "" {
		for _, c := range strings.Split(*cidrFlag, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				cidrs = append(cidrs, c)
			}
		}
		*mode = "custom"
	} else {
		switch strings.ToLower(*mode) {
		case "masque":
			cidrs = masqueCIDRs
		case "api":
			cidrs = apiCIDRs
			if *sni == "zero-trust-client.cloudflareclient.com" {
				*sni = "api.cloudflareclient.com"
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
			os.Exit(1)
		}
	}

	// 枚举目标
	var targets []string
	for _, cidr := range cidrs {
		ips, err := sampleFromCIDR(cidr, *sampleN)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: skip %s: %v\n", cidr, err)
			continue
		}
		for _, ip := range ips {
			targets = append(targets, fmt.Sprintf("%s:%d", ip, *port))
		}
	}

	fmt.Fprintf(os.Stderr, "Mode=%s SNI=%s Port=%d Targets=%d Rounds=%d Concurrency=%d Timeout=%s\n",
		*mode, *sni, *port, len(targets), *rounds, *concurrency, timeout)

	// 并发探测（每个 IP 串行多轮、不同 IP 之间并发）
	ctx := context.Background()
	var (
		mu      sync.Mutex
		results []ProbeResult
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, *concurrency)

	for _, addr := range targets {
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := probeMultiRound(ctx, a, *sni, timeout, *rounds)

			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(addr)
	}
	wg.Wait()

	// 排序：有回应的按平均延时升序，全超时的放最后
	sort.Slice(results, func(i, j int) bool {
		li, lj := results[i].AvgLatency, results[j].AvgLatency
		if li == 0 && lj == 0 {
			return results[i].Addr < results[j].Addr
		}
		if li == 0 {
			return false
		}
		if lj == 0 {
			return true
		}
		return li < lj
	})

	// 输出
	showCount := *topN
	if showCount > len(results) {
		showCount = len(results)
	}

	fmt.Printf("\n%-24s  %8s  %8s  %8s  %5s  %s\n", "ENDPOINT", "AVG", "MIN", "MAX", "OK/N", "STATUS")
	fmt.Println(strings.Repeat("-", 85))

	for i := 0; i < showCount; i++ {
		r := results[i]
		status := "OK"
		if r.LastErr != "" {
			if len(r.LastErr) > 30 {
				status = r.LastErr[:30] + "..."
			} else {
				status = r.LastErr
			}
		}
		okRatio := fmt.Sprintf("%d/%d", r.Responded, r.Rounds)
		if r.AvgLatency > 0 {
			fmt.Printf("%-24s  %6.1fms  %6.1fms  %6.1fms  %5s  %s\n",
				r.Addr,
				float64(r.AvgLatency)/float64(time.Millisecond),
				float64(r.MinLatency)/float64(time.Millisecond),
				float64(r.MaxLatency)/float64(time.Millisecond),
				okRatio,
				status)
		} else {
			fmt.Printf("%-24s  %8s  %8s  %8s  %5s  %s\n",
				r.Addr, "TIMEOUT", "-", "-", okRatio, status)
		}
	}

	// 统计
	var responded, timedOut int
	var totalCrypto, totalConnRefused, totalTimeout, totalOther int
	for _, r := range results {
		if r.AvgLatency > 0 {
			responded++
		} else {
			timedOut++
		}
		totalCrypto += r.CryptoErr
		totalConnRefused += r.ConnRefused
		totalTimeout += r.TimeoutErr
		totalOther += r.OtherErr
	}
	fmt.Fprintf(os.Stderr, "\n=== Summary ===")
	fmt.Fprintf(os.Stderr, "\nTotal IPs:              %d\n", len(results))
	fmt.Fprintf(os.Stderr, "Responded (RTT>0):      %d\n", responded)
	fmt.Fprintf(os.Stderr, "No response (RTT=0):    %d\n", timedOut)
	fmt.Fprintf(os.Stderr, "--- Error Breakdown (per-round) ---\n")
	fmt.Fprintf(os.Stderr, "CRYPTO_ERROR (valid):    %d\n", totalCrypto)
	fmt.Fprintf(os.Stderr, "Connection Refused:     %d\n", totalConnRefused)
	fmt.Fprintf(os.Stderr, "Timeout (no response):  %d\n", totalTimeout)
	fmt.Fprintf(os.Stderr, "Other Error:            %d\n", totalOther)
}
