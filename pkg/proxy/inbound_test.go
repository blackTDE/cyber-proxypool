package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"cyberproxypool/pkg/dialer"
	"cyberproxypool/pkg/model"
	xproxy "golang.org/x/net/proxy"
)

func TestInboundDualProtocol(t *testing.T) {
	// 1. Start a mock echo target server
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen mock server: %v", err)
	}
	defer echoLn.Close()
	echoAddr := echoLn.Addr().String()

	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				io.Copy(conn, conn)
			}(c)
		}
	}()

	// 2. Start inbound listener with DirectDialer
	stats := &model.NodeStats{}
	direct := &mockDirectDialer{}
	provider := NewFixedDialerProvider(direct, stats)

	inbound, err := NewInboundListener("127.0.0.1:0", provider)
	if err != nil {
		t.Fatalf("failed to start inbound: %v", err)
	}
	defer inbound.Close()

	proxyAddr := inbound.Addr()

	// 3. Test via SOCKS5 client
	socksDialer, err := xproxy.SOCKS5("tcp", proxyAddr, nil, xproxy.Direct)
	if err != nil {
		t.Fatalf("failed to create socks5 dialer: %v", err)
	}

	conn, err := socksDialer.Dial("tcp", echoAddr)
	if err != nil {
		t.Fatalf("failed to dial via socks5: %v", err)
	}
	msg := []byte("hello-cyber-proxy")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("socks5 write failed: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("socks5 read failed: %v", err)
	}
	conn.Close()
	if string(buf) != string(msg) {
		t.Errorf("socks5 echo mismatch: expected '%s', got '%s'", msg, buf)
	}

	// 4. Test via HTTP CONNECT client
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	if err != nil {
		t.Fatalf("failed to parse proxy url: %v", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	// Start a mock HTTP server to test HTTP CONNECT
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start http server: %v", err)
	}
	defer httpLn.Close()

	go http.Serve(httpLn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("cyber-ok"))
	}))

	httpURL := fmt.Sprintf("http://%s/test", httpLn.Addr().String())
	resp, err := httpClient.Get(httpURL)
	if err != nil {
		t.Fatalf("failed to get via http proxy: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read http proxy resp: %v", err)
	}
	if string(body) != "cyber-ok" {
		t.Errorf("http proxy body mismatch: %s", body)
	}
}

type mockDirectDialer struct{}

func (d *mockDirectDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	var netDialer net.Dialer
	return netDialer.DialContext(ctx, network, target)
}

var _ dialer.OutboundDialer = (*mockDirectDialer)(nil)
