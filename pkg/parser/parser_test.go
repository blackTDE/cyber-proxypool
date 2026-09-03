package parser

import (
	"encoding/base64"
	"testing"

	"cyberproxypool/pkg/model"
)

func TestParseClashYAML(t *testing.T) {
	yamlContent := `
proxies:
  - name: "🇭🇰 Hong Kong 01 [Trojan]"
    type: trojan
    server: hk01.example.com
    port: 443
    password: pass-trojan-123
    sni: hk01.example.com
    skip-cert-verify: true
  - name: "🇯🇵 Tokyo Shadowsocks"
    type: ss
    server: jp01.example.com
    port: 8388
    cipher: aes-256-gcm
    password: pass-ss-456
  - name: "🇺🇸 US Los Angeles VMess"
    type: vmess
    server: us01.example.com
    port: 443
    uuid: b831381d-6324-4d53-ad4f-8cda48b30811
    alterId: 0
    cipher: auto
    tls: true
    network: ws
    ws-opts:
      path: /vmess
      headers:
        Host: us01.example.com
  - name: "🇸🇬 Singapore VLESS"
    type: vless
    server: sg01.example.com
    port: 443
    uuid: 550e8400-e29b-41d4-a716-446655440000
    tls: true
    network: ws
`

	nodes, format, err := ParseContent(yamlContent, "sub-1", "TestSub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if format != "clash" {
		t.Fatalf("expected format 'clash', got '%s'", format)
	}

	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(nodes))
	}

	// Verify Node 1: Trojan
	if nodes[0].Protocol != model.ProtoTrojan || nodes[0].Port != 443 || nodes[0].Country != "Hong Kong" {
		t.Errorf("node 0 mismatch: %+v", nodes[0])
	}

	// Verify Node 2: SS
	if nodes[1].Protocol != model.ProtoShadowsocks || nodes[1].Cipher != "aes-256-gcm" || nodes[1].Country != "Japan" {
		t.Errorf("node 1 mismatch: %+v", nodes[1])
	}

	// Verify Node 3: VMess
	if nodes[2].Protocol != model.ProtoVMess || nodes[2].Path != "/vmess" || nodes[2].Country != "United States" {
		t.Errorf("node 2 mismatch: %+v", nodes[2])
	}

	// Verify Node 4: VLESS
	if nodes[3].Protocol != model.ProtoVLESS || nodes[3].Country != "Singapore" {
		t.Errorf("node 3 mismatch: %+v", nodes[3])
	}
}

func TestParseBase64Subscriptions(t *testing.T) {
	uris := "trojan://secret123@hk.node.com:443?sni=hk.node.com#%F0%9F%87%AD%F0%9F%87%B0%20HK%20Node\n" +
		"vless://550e8400-e29b-41d4-a716-446655440000@sg.node.com:443?security=tls&sni=sg.node.com#%F0%9F%87%B8%F0%9F%87%AC%20SG%20Node\n" +
		"socks5://alice:bob@jp.node.com:1080#%F0%9F%87%AF%F0%9F%87%B5%20JP%20Socks"

	b64 := base64.StdEncoding.EncodeToString([]byte(uris))

	nodes, format, err := ParseContent(b64, "sub-2", "B64Sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if format != "base64" {
		t.Fatalf("expected format 'base64', got '%s'", format)
	}

	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	if nodes[0].Protocol != model.ProtoTrojan || nodes[0].Country != "Hong Kong" {
		t.Errorf("node 0 mismatch: %+v", nodes[0])
	}
	if nodes[1].Protocol != model.ProtoVLESS || nodes[1].Country != "Singapore" {
		t.Errorf("node 1 mismatch: %+v", nodes[1])
	}
	if nodes[2].Protocol != model.ProtoSocks5 || nodes[2].Country != "Japan" {
		t.Errorf("node 2 mismatch: %+v", nodes[2])
	}
}
