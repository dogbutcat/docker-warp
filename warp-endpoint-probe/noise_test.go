package main

import "testing"

func TestBuildHandshakeInitiation(t *testing.T) {
	packet, err := buildHandshakeInitiation()
	if err != nil {
		t.Fatalf("buildHandshakeInitiation failed: %v", err)
	}
	if len(packet) != wgHandshakeInitiationSize {
		t.Fatalf("unexpected packet size: got=%d want=%d", len(packet), wgHandshakeInitiationSize)
	}
	if packet[0] != wgMessageTypeHandshakeInitiation {
		t.Fatalf("unexpected message type: got=%d want=%d", packet[0], wgMessageTypeHandshakeInitiation)
	}
}
