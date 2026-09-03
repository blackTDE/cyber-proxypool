package tester

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"cyberproxypool/pkg/model"
)

func TestExtractIPFromBody(t *testing.T) {
	// JSON with ip
	ip1 := extractIPFromBody([]byte(`{"ip": "203.0.113.195"}`))
	if ip1 != "203.0.113.195" {
		t.Errorf("expected 203.0.113.195, got %s", ip1)
	}

	// JSON with origin (httpbin)
	ip2 := extractIPFromBody([]byte(`{"origin": "198.51.100.1, 10.0.0.1"}`))
	if ip2 != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %s", ip2)
	}

	// Plain text
	ip3 := extractIPFromBody([]byte(`ip=192.0.2.55`))
	if ip3 != "192.0.2.55" {
		t.Errorf("expected 192.0.2.55, got %s", ip3)
	}
}

func TestTesterDirect(t *testing.T) {
	// Start mock http server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ip":"127.0.0.1"}`))
	}))

	serverURL := fmt.Sprintf("http://%s", ln.Addr().String())
	node := &model.Node{
		ID:       "n1",
		Name:     "Local Mock",
		Protocol: model.ProtoHTTP,
		Server:   "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
	}

	tester := NewTester(serverURL, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Note: testing a node whose dialer fails or direct test
	res := tester.TestNode(ctx, node, serverURL)
	if res.TestedAt.IsZero() {
		t.Errorf("expected testedAt to be set")
	}
}
