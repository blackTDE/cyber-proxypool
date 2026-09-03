package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"cyberproxypool/pkg/model"
	"cyberproxypool/pkg/proxy"
	"cyberproxypool/pkg/storage"
	"cyberproxypool/pkg/tester"
)

func setupTestServer(t *testing.T) (*Server, *http.ServeMux, string) {
	tempDir, err := os.MkdirTemp("", "cyberproxy-api-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	store, err := storage.NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	tunnel := proxy.NewTunnelPool(model.DefaultAppConfig().Tunnel, "127.0.0.1")
	mgr := proxy.NewManager("127.0.0.1", 30000, tunnel)
	tst := tester.NewTester("", 2)

	srv := NewServer(store, mgr, tunnel, tst)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	return srv, mux, tempDir
}

func TestAPIRoutes(t *testing.T) {
	_, mux, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	// 1. Test GET /api/status
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var status model.SystemStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if status.Version != "1.0.0" {
		t.Errorf("version mismatch: %s", status.Version)
	}

	// 2. Test POST /api/subscriptions with Clash YAML content
	clashYaml := `
proxies:
  - name: "🇭🇰 Hong Kong 01"
    type: trojan
    server: hk01.node.com
    port: 443
    password: secret-pass-1
  - name: "🇯🇵 Tokyo 02"
    type: ss
    server: jp02.node.com
    port: 8388
    cipher: aes-256-gcm
    password: secret-pass-2
`
	subReqBody, _ := json.Marshal(map[string]string{
		"name":    "My Test Sub",
		"content": clashYaml,
	})
	subReq := httptest.NewRequest("POST", "/api/subscriptions", bytes.NewReader(subReqBody))
	subRec := httptest.NewRecorder()
	mux.ServeHTTP(subRec, subReq)

	if subRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", subRec.Code, subRec.Body.String())
	}

	// 3. Test GET /api/nodes
	nodesReq := httptest.NewRequest("GET", "/api/nodes", nil)
	nodesRec := httptest.NewRecorder()
	mux.ServeHTTP(nodesRec, nodesReq)

	if nodesRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", nodesRec.Code)
	}

	var nodes []*model.Node
	if err := json.NewDecoder(nodesRec.Body).Decode(&nodes); err != nil {
		t.Fatalf("failed to decode nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	// 4. Test GET /api/tunnel
	tunReq := httptest.NewRequest("GET", "/api/tunnel", nil)
	tunRec := httptest.NewRecorder()
	mux.ServeHTTP(tunRec, tunReq)

	if tunRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", tunRec.Code)
	}
}
