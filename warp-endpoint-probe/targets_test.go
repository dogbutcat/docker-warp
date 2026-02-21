package main

import (
	"strings"
	"testing"
)

func TestExpandTargetsCount(t *testing.T) {
	// IPv4 only — consumer pool (254 IPs × 4 ports = 1016)
	consumerPool, err := SelectPool("tunnel", "consumer", "wireguard", false)
	if err != nil {
		t.Fatalf("select consumer pool: %v", err)
	}
	consumerEndpoints, err := ExpandTargets(consumerPool, false, 0)
	if err != nil {
		t.Fatalf("expand consumer endpoints: %v", err)
	}
	if len(consumerEndpoints) != 1016 {
		t.Fatalf("unexpected consumer endpoints count: got=%d want=1016", len(consumerEndpoints))
	}

	// masque IPv4 only (254 IPs × 1 port = 254)
	masquePool, err := SelectPool("tunnel", "masque", "masque", true)
	if err != nil {
		t.Fatalf("select masque pool: %v", err)
	}
	masqueV4, err := ExpandTargets(masquePool, false, 0)
	if err != nil {
		t.Fatalf("expand masque v4: %v", err)
	}
	wantV4 := 254 * 1
	if len(masqueV4) != wantV4 {
		t.Fatalf("masque v4 count: got=%d want=%d", len(masqueV4), wantV4)
	}
	for _, ep := range masqueV4 {
		if strings.Contains(ep.IP, ":") {
			t.Fatalf("IPv6 leaked in v4-only mode: %s", ep.IP)
		}
	}

	// masque IPv4 + IPv6 (254 + sampled IPv6)
	masqueAll, err := ExpandTargets(masquePool, true, 0)
	if err != nil {
		t.Fatalf("expand masque v4+v6: %v", err)
	}
	wantMin := 254 + 1024
	if len(masqueAll) < wantMin {
		t.Fatalf("masque v4+v6 count: got=%d wantMin=%d", len(masqueAll), wantMin)
	}

	hasIPv6 := false
	for _, ep := range masqueAll {
		if strings.Contains(ep.IP, ":") {
			hasIPv6 = true
			addr := ep.Address()
			if !strings.HasPrefix(addr, "[") {
				t.Fatalf("IPv6 address missing brackets: %s", addr)
			}
			break
		}
	}
	if !hasIPv6 {
		t.Fatal("no IPv6 endpoints found in masque v4+v6 mode")
	}
}

func TestExpandTargetsWithSample(t *testing.T) {
	// 使用 sample=3 时，每个 CIDR 只取 3 个 IP
	apiPool, err := SelectPool("api", "", "", false)
	if err != nil {
		t.Fatalf("select api pool: %v", err)
	}
	apiEndpoints, err := ExpandTargets(apiPool, false, 3)
	if err != nil {
		t.Fatalf("expand api endpoints: %v", err)
	}
	// 48 CIDRs × 3 samples × 1 port = 144
	wantAPI := 48 * 3 * 1
	if len(apiEndpoints) != wantAPI {
		t.Fatalf("api sample count: got=%d want=%d", len(apiEndpoints), wantAPI)
	}
}
