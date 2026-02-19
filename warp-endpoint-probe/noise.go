package main

// WireGuard Noise IK Handshake Initiation packet construction.
//
// Reference: https://www.wireguard.com/protocol/
// Message format (148 bytes total):
//   - Type (4 bytes): 0x01000000
//   - Sender Index (4 bytes): random
//   - Unencrypted Ephemeral (32 bytes): initiator ephemeral public key
//   - Encrypted Static (48 bytes): AEAD(initiator static pub, 0 nonce)
//   - Encrypted Timestamp (28 bytes): AEAD(TAI64N timestamp, 1 nonce)
//   - MAC1 (16 bytes): keyed hash
//   - MAC2 (16 bytes): zero (no cookie)
//
// For probing, we only need Cloudflare to recognize this as a valid
// WireGuard initiation and respond with a Handshake Response (92 bytes).
// We use an ephemeral keypair and the known Cloudflare public key.

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/blake2s"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

const (
	// WireGuard protocol constants
	wgMessageTypeHandshakeInitiation = 1
	wgHandshakeInitiationSize        = 148
	wgHandshakeResponseSize          = 92

	// Noise protocol constants
	noiseConstruction = "Noise_IKpsk2_25519_ChaChaPoly_BLAKE2s"
	wgIdentifier      = "WireGuard v1 zx2c4 Jason@zx2c4.com"
	wgLabelMAC1       = "mac1----"
)

// Cloudflare WARP WireGuard public key (base64: bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=)
var cloudflarePublicKey = [32]byte{
	0x6e, 0x65, 0xce, 0x0b, 0xe1, 0x75, 0x17, 0x11,
	0x0c, 0x17, 0xd7, 0x72, 0x88, 0xad, 0x87, 0xe7,
	0xfd, 0x52, 0x52, 0xdc, 0xc7, 0xd0, 0x9b, 0x95,
	0xa3, 0x9d, 0x61, 0xdb, 0x03, 0xdf, 0x83, 0x2a,
}

// ProbeWireGuardHandshake sends a 148-byte handshake initiation packet and
// waits for a WireGuard response packet to measure RTT.
func ProbeWireGuardHandshake(ctx context.Context, endpoint Endpoint, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	packet, err := buildHandshakeInitiation()
	if err != nil {
		return 0, fmt.Errorf("build wireguard initiation: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{Timeout: timeout}
	connRaw, err := dialer.DialContext(probeCtx, "udp", endpoint.Address())
	if err != nil {
		return 0, fmt.Errorf("dial udp %s: %w", endpoint.Address(), err)
	}
	defer connRaw.Close()

	conn, ok := connRaw.(*net.UDPConn)
	if !ok {
		return 0, fmt.Errorf("unexpected conn type for %s", endpoint.Address())
	}

	_ = conn.SetDeadline(time.Now().Add(timeout))

	start := time.Now()
	if _, err = conn.Write(packet); err != nil {
		return 0, fmt.Errorf("send handshake initiation %s: %w", endpoint.Address(), err)
	}

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	latency := time.Since(start)
	if err != nil {
		return 0, fmt.Errorf("read handshake response %s: %w", endpoint.Address(), err)
	}
	if n < 4 || buf[0] != 2 {
		return 0, fmt.Errorf("invalid handshake response %s: size=%d type=%d", endpoint.Address(), n, buf[0])
	}

	return latency, nil
}

// buildHandshakeInitiation constructs a 148-byte WireGuard Handshake
// Initiation message targeting the Cloudflare WARP endpoint.
func buildHandshakeInitiation() ([]byte, error) {
	// Generate ephemeral keypair
	var ephPriv, ephPub [32]byte
	if _, err := rand.Read(ephPriv[:]); err != nil {
		return nil, err
	}
	curve25519.ScalarBaseMult(&ephPub, &ephPriv)

	// Generate initiator static keypair (throwaway, just for probing)
	var staticPriv, staticPub [32]byte
	if _, err := rand.Read(staticPriv[:]); err != nil {
		return nil, err
	}
	curve25519.ScalarBaseMult(&staticPub, &staticPriv)

	// Initialize chaining key and hash per Noise IK
	// h = HASH(CONSTRUCTION)
	// ck = h
	// h = HASH(h || IDENTIFIER)
	// h = HASH(h || responder_public_key)
	constructionHash := blake2sHash([]byte(noiseConstruction))
	ck := constructionHash
	h := blake2sHash(constructionHash[:], []byte(wgIdentifier))
	h = blake2sHash(h[:], cloudflarePublicKey[:])

	// msg.type = 1
	// msg.sender_index = random 4 bytes
	msg := make([]byte, wgHandshakeInitiationSize)
	msg[0] = wgMessageTypeHandshakeInitiation
	// bytes 1-3 are reserved (zero)
	if _, err := rand.Read(msg[4:8]); err != nil {
		return nil, err
	}

	// msg.unencrypted_ephemeral = ephemeral_public
	// h = HASH(h || msg.unencrypted_ephemeral)
	// ck, k = HKDF(ck, msg.unencrypted_ephemeral)
	copy(msg[8:40], ephPub[:])
	h = blake2sHash(h[:], ephPub[:])

	ckNew, k := hkdf2(ck[:], ephPub[:])
	ck = ckNew
	_ = k // k is used for KDF, not directly here

	// DH: ephemeral_private × responder_public
	ss, err := curve25519.X25519(ephPriv[:], cloudflarePublicKey[:])
	if err != nil {
		return nil, err
	}
	ck, k = hkdf2(ck[:], ss)

	// msg.encrypted_static = AEAD(k, 0, static_public, h)
	aead, err := chacha20poly1305.New(k[:])
	if err != nil {
		return nil, err
	}
	var nonce [12]byte // zero nonce
	encrypted := aead.Seal(nil, nonce[:], staticPub[:], h[:])
	copy(msg[40:88], encrypted)

	// h = HASH(h || msg.encrypted_static)
	h = blake2sHash(h[:], msg[40:88])

	// DH: static_private × responder_public
	ss2, err := curve25519.X25519(staticPriv[:], cloudflarePublicKey[:])
	if err != nil {
		return nil, err
	}
	ck, k = hkdf2(ck[:], ss2)

	// msg.encrypted_timestamp = AEAD(k, 0, TAI64N(now), h)
	timestamp := tai64nNow()
	var nonce1 [12]byte
	nonce1[11] = 1 // nonce = 1 (little-endian counter)

	// Need new AEAD with new key
	aead2, err := chacha20poly1305.New(k[:])
	if err != nil {
		return nil, err
	}
	encryptedTS := aead2.Seal(nil, nonce1[:], timestamp[:], h[:])
	copy(msg[88:116], encryptedTS)

	// h = HASH(h || msg.encrypted_timestamp)
	h = blake2sHash(h[:], msg[88:116])
	_ = ck // ck is carried forward for response processing (not needed for probe)

	// MAC1 = keyed_hash(HASH(LABEL_MAC1 || responder_public), msg[0:116])
	mac1Key := blake2sHash([]byte(wgLabelMAC1), cloudflarePublicKey[:])
	mac1 := blake2s128(mac1Key[:], msg[:116])
	copy(msg[116:132], mac1[:])

	// MAC2 = zeros (no cookie)
	// msg[132:148] already zero

	return msg, nil
}

// --- Cryptographic helpers ---

func blake2sHash(data ...[]byte) [32]byte {
	h, _ := blake2s.New256(nil) // unkeyed hash
	for _, d := range data {
		h.Write(d)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func blake2s128(key []byte, data []byte) [16]byte {
	h, _ := blake2s.New128(key)
	h.Write(data)
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

// hkdf2 extracts two 32-byte keys using BLAKE2s-based HKDF.
// This follows the WireGuard specification's KDF function.
func hkdf2(ck, input []byte) ([32]byte, [32]byte) {
	// PRK = HMAC-BLAKE2s(ck, input)
	prk := hmacBlake2s(ck, input)

	// T1 = HMAC-BLAKE2s(PRK, 0x01)
	t1 := hmacBlake2s(prk[:], []byte{0x01})

	// T2 = HMAC-BLAKE2s(PRK, T1 || 0x02)
	t2Input := make([]byte, 33)
	copy(t2Input, t1[:])
	t2Input[32] = 0x02
	t2 := hmacBlake2s(prk[:], t2Input)

	return t1, t2
}

func hmacBlake2s(key, data []byte) [32]byte {
	// HMAC using BLAKE2s
	const blockSize = 64
	if len(key) > blockSize {
		h := blake2sHash(key)
		key = h[:]
	}

	ipad := make([]byte, blockSize)
	opad := make([]byte, blockSize)
	copy(ipad, key)
	copy(opad, key)
	for i := 0; i < blockSize; i++ {
		ipad[i] ^= 0x36
		opad[i] ^= 0x5c
	}

	inner := blake2sHash(append(ipad, data...))
	return blake2sHash(append(opad, inner[:]...))
}

// tai64nNow returns the current time as a 12-byte TAI64N timestamp.
func tai64nNow() [12]byte {
	now := time.Now()
	var ts [12]byte
	// TAI64 = 2^62 + Unix epoch seconds
	secs := uint64(now.Unix()) + 4611686018427387914
	binary.BigEndian.PutUint64(ts[:8], secs)
	binary.BigEndian.PutUint32(ts[8:12], uint32(now.Nanosecond()))
	return ts
}
