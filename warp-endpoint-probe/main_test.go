package main

import "testing"

func TestInferTunnelTarget(t *testing.T) {
	testCases := []struct {
		name     string
		protocol string
		mdm      bool
		expected string
	}{
		{name: "consumer_when_not_mdm", protocol: "wireguard", mdm: false, expected: "consumer"},
		{name: "masque_when_mdm_and_masque", protocol: "masque", mdm: true, expected: "masque"},
		{name: "wireguard_when_mdm_and_not_masque", protocol: "wireguard", mdm: true, expected: "wireguard"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual := inferTunnelTarget(testCase.protocol, testCase.mdm)
			if actual != testCase.expected {
				t.Fatalf("unexpected target: got=%s want=%s", actual, testCase.expected)
			}
		})
	}
}
