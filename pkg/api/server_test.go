package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	srv, mux, tempDir := setupTestServer(t)
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

	// 5. Test POST /api/nodes/batch-start
	batchBody, _ := json.Marshal(map[string]any{
		"ids": []string{nodes[0].ID, nodes[1].ID},
	})
	batchStartReq := httptest.NewRequest("POST", "/api/nodes/batch-start", bytes.NewReader(batchBody))
	batchStartRec := httptest.NewRecorder()
	mux.ServeHTTP(batchStartRec, batchStartReq)
	if batchStartRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on batch-start, got %d (body: %s)", batchStartRec.Code, batchStartRec.Body.String())
	}

	// 6. Test POST /api/nodes/batch-stop
	batchStopReq := httptest.NewRequest("POST", "/api/nodes/batch-stop", bytes.NewReader(batchBody))
	batchStopRec := httptest.NewRecorder()
	mux.ServeHTTP(batchStopRec, batchStopReq)
	if batchStopRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on batch-stop, got %d (body: %s)", batchStopRec.Code, batchStopRec.Body.String())
	}

	// 7. Set latency on nodes and test GET /api/proxy/fastest
	store := srv.store
	store.UpdateNodeRuntime(nodes[0].ID, false, 0, 150, "1.1.1.1", "")
	store.UpdateNodeRuntime(nodes[1].ID, false, 0, 85, "2.2.2.2", "")

	// Test GET /api/proxy/fastest (JSON, auto_start=true)
	fastestReq := httptest.NewRequest("GET", "/api/proxy/fastest", nil)
	fastestRec := httptest.NewRecorder()
	mux.ServeHTTP(fastestRec, fastestReq)
	if fastestRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/proxy/fastest, got %d", fastestRec.Code)
	}
	var fastItem ProxyItemResponse
	if err := json.NewDecoder(fastestRec.Body).Decode(&fastItem); err != nil {
		t.Fatalf("failed to decode fastest proxy response: %v", err)
	}
	if fastItem.ID != nodes[1].ID {
		t.Errorf("expected fastest node ID %s, got %s", nodes[1].ID, fastItem.ID)
	}
	if !fastItem.IsRunning || fastItem.InboundPort <= 0 {
		t.Errorf("expected fastest node to be auto-started, got running=%v port=%d", fastItem.IsRunning, fastItem.InboundPort)
	}

	// Test GET /api/proxy/fastest?format=text
	fastestTextReq := httptest.NewRequest("GET", "/api/proxy/fastest?format=text", nil)
	fastestTextRec := httptest.NewRecorder()
	mux.ServeHTTP(fastestTextRec, fastestTextReq)
	if fastestTextRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/proxy/fastest?format=text, got %d", fastestTextRec.Code)
	}
	textBody := strings.TrimSpace(fastestTextRec.Body.String())
	if !strings.HasPrefix(textBody, "http://") {
		t.Errorf("expected http:// prefix in text mode, got %s", textBody)
	}

	// 8. Test GET /api/proxy/get (alias)
	getAliasReq := httptest.NewRequest("GET", "/api/proxy/get?format=socks5", nil)
	getAliasRec := httptest.NewRecorder()
	mux.ServeHTTP(getAliasRec, getAliasReq)
	if getAliasRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/proxy/get, got %d", getAliasRec.Code)
	}
	socks5Body := strings.TrimSpace(getAliasRec.Body.String())
	if !strings.HasPrefix(socks5Body, "socks5://") {
		t.Errorf("expected socks5:// prefix, got %s", socks5Body)
	}

	// 9. Test GET /api/proxy/random
	randomReq := httptest.NewRequest("GET", "/api/proxy/random", nil)
	randomRec := httptest.NewRecorder()
	mux.ServeHTTP(randomRec, randomReq)
	if randomRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/proxy/random, got %d", randomRec.Code)
	}

	// 10. Test GET /api/proxy/list
	listReq := httptest.NewRequest("GET", "/api/proxy/list?running_only=false", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/proxy/list, got %d", listRec.Code)
	}
	var proxyList []ProxyItemResponse
	if err := json.NewDecoder(listRec.Body).Decode(&proxyList); err != nil {
		t.Fatalf("failed to decode proxy list: %v", err)
	}
	if len(proxyList) != 2 {
		t.Errorf("expected 2 proxies in list, got %d", len(proxyList))
	}

	// 11. Test GET /api/proxy/tunnel
	proxyTunReq := httptest.NewRequest("GET", "/api/proxy/tunnel", nil)
	proxyTunRec := httptest.NewRecorder()
	mux.ServeHTTP(proxyTunRec, proxyTunReq)
	if proxyTunRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/proxy/tunnel, got %d", proxyTunRec.Code)
	}
	var tunResp TunnelProxyResponse
	if err := json.NewDecoder(proxyTunRec.Body).Decode(&tunResp); err != nil {
		t.Fatalf("failed to decode tunnel response: %v", err)
	}
	if tunResp.Port != 10808 {
		t.Errorf("expected tunnel port 10808, got %d", tunResp.Port)
	}
}
