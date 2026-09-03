package dialer

import (
	"encoding/binary"
	"net"
	"testing"

	"cyberproxypool/pkg/model"
)

func TestEncodeTargetAddr(t *testing.T) {
	// 1. Test IPv4
	b4, err := EncodeTargetAddr("1.2.3.4:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b4[0] != 0x01 {
		t.Errorf("expected atyp 0x01 for ipv4, got 0x%02x", b4[0])
	}
	if !net.IP(b4[1:5]).Equal(net.ParseIP("1.2.3.4").To4()) {
		t.Errorf("ipv4 bytes mismatch")
	}
	port := binary.BigEndian.Uint16(b4[5:7])
	if port != 8080 {
		t.Errorf("expected port 8080, got %d", port)
	}

	// 2. Test Domain
	bDom, err := EncodeTargetAddr("api.ipify.org:443")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bDom[0] != 0x03 {
		t.Errorf("expected atyp 0x03 for domain, got 0x%02x", bDom[0])
	}
	dLen := int(bDom[1])
	domain := string(bDom[2 : 2+dLen])
	if domain != "api.ipify.org" {
		t.Errorf("expected 'api.ipify.org', got '%s'", domain)
	}
	portDom := binary.BigEndian.Uint16(bDom[2+dLen : 4+dLen])
	if portDom != 443 {
		t.Errorf("expected port 443, got %d", portDom)
	}
}

func TestNewDialerFromNode(t *testing.T) {
	nodeTrojan := &model.Node{
		Protocol: model.ProtoTrojan,
		Server:   "127.0.0.1",
		Port:     443,
		Password: "password123",
	}
	d, err := NewDialerFromNode(nodeTrojan)
	if err != nil {
		t.Fatalf("failed to create trojan dialer: %v", err)
	}
	if d == nil {
		t.Fatalf("expected non-nil dialer")
	}

	nodeSS := &model.Node{
		Protocol: model.ProtoShadowsocks,
		Server:   "127.0.0.1",
		Port:     8388,
		Cipher:   "aes-256-gcm",
		Password: "password123",
	}
	d2, err := NewDialerFromNode(nodeSS)
	if err != nil {
		t.Fatalf("failed to create shadowsocks dialer: %v", err)
	}
	if d2 == nil {
		t.Fatalf("expected non-nil dialer")
	}
}
