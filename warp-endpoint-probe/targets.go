package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
)

const (
	DefaultSNI    = "api.cloudflareclient.com"
	MasqueSNI     = "zero-trust-client.cloudflareclient.com"
)

type ProbeType string

const (
	ProbeWireGuard ProbeType = "wireguard"
	ProbeQUIC      ProbeType = "quic"
	ProbeHTTPS     ProbeType = "https"
)

var ErrUnsupportedProbe = errors.New("unsupported probe type")

type TargetPool struct {
	Name  string
	CIDR  string
	CIDRs []string
	Ports []int
	Probe ProbeType
	SNI   string
}

type Endpoint struct {
	IP       string
	Port     int
	Probe    ProbeType
	SNI      string
	PoolName string
	PoolCIDR string
}

func (endpoint Endpoint) Address() string {
	if strings.Contains(endpoint.IP, ":") {
		return fmt.Sprintf("[%s]:%d", endpoint.IP, endpoint.Port)
	}
	return fmt.Sprintf("%s:%d", endpoint.IP, endpoint.Port)
}

var tunnelTargets = map[string]TargetPool{
	"consumer": {
		Name:  "consumer",
		CIDR:  "162.159.192.0/24",
		Ports: []int{2408, 500, 1701, 4500},
		Probe: ProbeWireGuard,
	},
	"wireguard": {
		Name:  "wireguard",
		CIDR:  "162.159.193.0/24",
		Ports: []int{2408, 500, 1701, 4500},
		Probe: ProbeWireGuard,
	},
	"masque": {
		Name:  "masque",
		CIDRs: []string{"162.159.197.0/24", "2606:4700:102::/48"},
		Ports: []int{443, 500, 1701, 4500, 4443, 8443, 8095},
		Probe: ProbeQUIC,
		SNI:   MasqueSNI,
	},
}

var apiTargets = TargetPool{
	Name: "jdcloud",
	CIDRs: []string{
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
	},
	Ports: []int{443},
	Probe: ProbeHTTPS,
	SNI:   DefaultSNI,
}

func SelectPool(mode string, target string, protocol string, mdm bool) (TargetPool, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	target = strings.ToLower(strings.TrimSpace(target))
	protocol = strings.ToLower(strings.TrimSpace(protocol))

	switch mode {
	case "api":
		return apiTargets, nil
	case "tunnel":
		if target == "" {
			target = inferTunnelTarget(protocol, mdm)
		}
		pool, exists := tunnelTargets[target]
		if !exists {
			return TargetPool{}, fmt.Errorf("unknown tunnel target: %s", target)
		}
		return pool, nil
	default:
		return TargetPool{}, fmt.Errorf("unsupported mode: %s", mode)
	}
}

func ExpandTargets(pool TargetPool, ipv6 bool) ([]Endpoint, error) {
	if len(pool.Ports) == 0 {
		return nil, fmt.Errorf("pool %s has no ports", pool.Name)
	}

	cidrs := make([]string, 0, len(pool.CIDRs)+1)
	if pool.CIDR != "" {
		cidrs = append(cidrs, pool.CIDR)
	}
	cidrs = append(cidrs, pool.CIDRs...)
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("pool %s has no cidr", pool.Name)
	}

	if !ipv6 {
		filtered := cidrs[:0]
		for _, c := range cidrs {
			if !strings.Contains(c, ":") {
				filtered = append(filtered, c)
			}
		}
		cidrs = filtered
		if len(cidrs) == 0 {
			return nil, fmt.Errorf("pool %s has no ipv4 cidr", pool.Name)
		}
	}

	endpoints := make([]Endpoint, 0)
	for _, cidr := range cidrs {
		hosts, err := expandCIDRHosts(cidr)
		if err != nil {
			return nil, err
		}
		for _, host := range hosts {
			for _, port := range pool.Ports {
				endpoints = append(endpoints, Endpoint{
					IP:       host,
					Port:     port,
					Probe:    pool.Probe,
					SNI:      pool.SNI,
					PoolName: pool.Name,
					PoolCIDR: cidr,
				})
			}
		}
	}
	return endpoints, nil
}

// IPv6 大段随机采样上限
const ipv6SampleSize = 1024

func expandCIDRHosts(cidr string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse cidr %s: %w", cidr, err)
	}

	ones, bits := ipNet.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 1 {
		return []string{}, nil
	}

	if bits == 32 {
		return expandIPv4Hosts(ip.To4(), hostBits)
	}
	return sampleIPv6Hosts(ipNet, hostBits)
}

func expandIPv4Hosts(ipv4 net.IP, hostBits int) ([]string, error) {
	base := binary.BigEndian.Uint32(ipv4)
	hostCount := uint32(1<<hostBits) - 2
	hosts := make([]string, 0, hostCount)
	for i := uint32(1); i <= hostCount; i++ {
		var value [4]byte
		binary.BigEndian.PutUint32(value[:], base+i)
		hosts = append(hosts, net.IPv4(value[0], value[1], value[2], value[3]).String())
	}
	return hosts, nil
}

func sampleIPv6Hosts(ipNet *net.IPNet, hostBits int) ([]string, error) {
	// 对地址空间取上限
	hostSpace := new(big.Int).Lsh(big.NewInt(1), uint(hostBits))
	sampleCount := ipv6SampleSize
	if hostSpace.Cmp(big.NewInt(int64(sampleCount))) < 0 {
		sampleCount = int(hostSpace.Int64()) - 2
	}
	if sampleCount <= 0 {
		return []string{}, nil
	}

	base := make(net.IP, len(ipNet.IP))
	copy(base, ipNet.IP)

	seen := make(map[string]struct{}, sampleCount)
	hosts := make([]string, 0, sampleCount)

	for len(hosts) < sampleCount {
		offset, err := rand.Int(rand.Reader, hostSpace)
		if err != nil {
			return nil, fmt.Errorf("random sample: %w", err)
		}
		if offset.Sign() == 0 {
			continue
		}

		ip := addOffset(base, offset)
		s := ip.String()
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		hosts = append(hosts, s)
	}
	return hosts, nil
}

func addOffset(base net.IP, offset *big.Int) net.IP {
	b := new(big.Int).SetBytes(base.To16())
	b.Add(b, offset)
	out := b.Bytes()
	ip := make(net.IP, 16)
	copy(ip[16-len(out):], out)
	return ip
}
