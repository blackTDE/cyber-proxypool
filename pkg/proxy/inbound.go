package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberproxypool/pkg/dialer"
	"cyberproxypool/pkg/model"
)

// DialerProvider provides an outbound dialer for each request
type DialerProvider interface {
	GetDialer() (dialer.OutboundDialer, *model.NodeStats, error)
}

// FixedDialerProvider wraps a single outbound dialer and node stats
type FixedDialerProvider struct {
	dialer dialer.OutboundDialer
	stats  *model.NodeStats
}

func NewFixedDialerProvider(d dialer.OutboundDialer, stats *model.NodeStats) *FixedDialerProvider {
	return &FixedDialerProvider{dialer: d, stats: stats}
}

func (p *FixedDialerProvider) GetDialer() (dialer.OutboundDialer, *model.NodeStats, error) {
	return p.dialer, p.stats, nil
}

// InboundListener listens on a local port and handles dual-protocol (HTTP & SOCKS5) proxy requests
type InboundListener struct {
	addr     string
	provider DialerProvider
	listener net.Listener
	closed   bool
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// NewInboundListener creates and starts a dual-protocol listener
func NewInboundListener(addr string, provider DialerProvider) (*InboundListener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	inbound := &InboundListener{
		addr:     addr,
		provider: provider,
		listener: ln,
	}

	go inbound.serve()
	return inbound, nil
}

func (l *InboundListener) Addr() string {
	if l.listener != nil {
		return l.listener.Addr().String()
	}
	return l.addr
}

func (l *InboundListener) Port() int {
	if l.listener != nil {
		if tcpAddr, ok := l.listener.Addr().(*net.TCPAddr); ok {
			return tcpAddr.Port
		}
	}
	return 0
}

func (l *InboundListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	err := l.listener.Close()
	l.mu.Unlock()

	l.wg.Wait()
	return err
}

func (l *InboundListener) serve() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}

		l.wg.Add(1)
		go func(c net.Conn) {
			defer l.wg.Done()
			defer c.Close()
			l.handleConn(c)
		}(conn)
	}
}

func (l *InboundListener) handleConn(clientConn net.Conn) {
	reader := bufio.NewReader(clientConn)

	// Peek first byte to distinguish SOCKS5 (0x05) from HTTP (C, G, P, H, ...)
	firstByte, err := reader.Peek(1)
	if err != nil {
		return
	}

	outboundDialer, stats, err := l.provider.GetDialer()
	if err != nil {
		return
	}

	if stats != nil {
		stats.IncrConn()
		defer stats.DecrConn()
	}

	if firstByte[0] == 0x05 {
		// SOCKS5 protocol
		l.handleSocks5(clientConn, reader, outboundDialer, stats)
	} else {
		// HTTP / HTTPS CONNECT protocol
		l.handleHTTP(clientConn, reader, outboundDialer, stats)
	}
}

// handleSocks5 implements RFC 1928 SOCKS5 server protocol
func (l *InboundListener) handleSocks5(clientConn net.Conn, reader *bufio.Reader, outbound dialer.OutboundDialer, stats *model.NodeStats) {
	// 1. Version identifier / method selection
	ver, err := reader.ReadByte()
	if err != nil || ver != 0x05 {
		return
	}

	nmethods, err := reader.ReadByte()
	if err != nil || nmethods == 0 {
		return
	}

	methods := make([]byte, int(nmethods))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}

	// Reply: version 5, method 0 (NO AUTH REQUIRED)
	if _, err := clientConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. Read SOCKS5 request: [VER(1)][CMD(1)][RSV(1)][ATYP(1)][ADDR(var)][PORT(2)]
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return
	}

	if header[0] != 0x05 || header[1] != 0x01 { // Only CMD 0x01 (CONNECT) supported
		// Command not supported
		clientConn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	atyp := header[3]
	var targetHost string

	switch atyp {
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	case 0x03: // Domain
		dLenByte, err := reader.ReadByte()
		if err != nil {
			return
		}
		domainBuf := make([]byte, int(dLenByte))
		if _, err := io.ReadFull(reader, domainBuf); err != nil {
			return
		}
		targetHost = string(domainBuf)
	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(reader, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	default:
		clientConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var portBuf [2]byte
	if _, err := io.ReadFull(reader, portBuf[:]); err != nil {
		return
	}
	targetPort := binary.BigEndian.Uint16(portBuf[:])
	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(int(targetPort)))

	// Connect to target via outbound dialer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	remoteConn, err := outbound.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		// Host unreachable (0x04) or Network unreachable (0x03)
		clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	// Reply SOCKS5 Success (0x00)
	if _, err := clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	pipeBidirectional(clientConn, remoteConn, stats)
}

// handleHTTP implements HTTP proxy server (both CONNECT tunneling and direct HTTP proxying)
func (l *InboundListener) handleHTTP(clientConn net.Conn, reader *bufio.Reader, outbound dialer.OutboundDialer, stats *model.NodeStats) {
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		// HTTPS Tunneling via CONNECT
		target := req.URL.Host
		if !strings.Contains(target, ":") {
			target = net.JoinHostPort(target, "443")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		remoteConn, err := outbound.DialContext(ctx, "tcp", target)
		if err != nil {
			clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
			return
		}
		defer remoteConn.Close()

		// Write 200 OK
		if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\nProxy-Agent: CyberProxyPool\r\n\r\n")); err != nil {
			return
		}

		pipeBidirectional(clientConn, remoteConn, stats)
	} else {
		// Plain HTTP Proxying (GET/POST with absolute URL)
		target := req.URL.Host
		if target == "" {
			target = req.Host
		}
		if !strings.Contains(target, ":") {
			target = net.JoinHostPort(target, "80")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		remoteConn, err := outbound.DialContext(ctx, "tcp", target)
		if err != nil {
			clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
			return
		}
		defer remoteConn.Close()

		// Strip proxy-specific headers and write request to remote
		req.Header.Del("Proxy-Connection")
		req.Header.Del("Proxy-Authorization")
		req.RequestURI = "" // RequestURI must be empty for Client.Do / Write

		// Convert absolute URL to relative for standard origin server
		if req.URL.IsAbs() {
			relURL, _ := url.Parse(req.URL.RequestURI())
			req.URL = relURL
		}

		if err := req.Write(remoteConn); err != nil {
			return
		}

		pipeBidirectional(clientConn, remoteConn, stats)
	}
}

// pipeBidirectional relays data between client and remote connection while recording stats
func pipeBidirectional(clientConn, remoteConn net.Conn, stats *model.NodeStats) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Remote (Upload)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := clientConn.Read(buf)
			if n > 0 {
				if _, wErr := remoteConn.Write(buf[:n]); wErr != nil {
					break
				}
				if stats != nil {
					stats.AddUpload(int64(n))
				}
			}
			if err != nil {
				break
			}
		}
		if tc, ok := remoteConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// Remote -> Client (Download)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := remoteConn.Read(buf)
			if n > 0 {
				if _, wErr := clientConn.Write(buf[:n]); wErr != nil {
					break
				}
				if stats != nil {
					stats.AddDownload(int64(n))
				}
			}
			if err != nil {
				break
			}
		}
		if tc, ok := clientConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}
