package dialer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberproxypool/pkg/model"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sagernet/sing-vmess"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/shadowsocks/go-shadowsocks2/core"
	"golang.org/x/net/proxy"
)

// OutboundDialer defines the interface for connecting to remote targets via a proxy node
type OutboundDialer interface {
	DialContext(ctx context.Context, network, target string) (net.Conn, error)
}

// DirectDialer connects directly without proxy
type DirectDialer struct {
	netDialer net.Dialer
}

func (d *DirectDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	return d.netDialer.DialContext(ctx, network, target)
}

// NewDialerFromNode creates an OutboundDialer based on the node's protocol and parameters
func NewDialerFromNode(n *model.Node) (OutboundDialer, error) {
	switch n.Protocol {
	case model.ProtoTrojan:
		return NewTrojanDialer(n), nil
	case model.ProtoShadowsocks:
		return NewShadowsocksDialer(n)
	case model.ProtoVLESS:
		return NewVLESSDialer(n)
	case model.ProtoSocks5:
		return NewSocks5Dialer(n)
	case model.ProtoHTTP:
		return NewHTTPDialer(n), nil
	case model.ProtoVMess:
		return NewVMessDialer(n)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", n.Protocol)
	}
}

// ==========================================
// Trojan Dialer
// ==========================================
type TrojanDialer struct {
	node      *model.Node
	hexHash   string
	netDialer net.Dialer
}

func NewTrojanDialer(n *model.Node) *TrojanDialer {
	h := sha256.Sum224([]byte(n.Password))
	hexHash := hex.EncodeToString(h[:])
	return &TrojanDialer{
		node:      n,
		hexHash:   hexHash,
		netDialer: net.Dialer{Timeout: 10 * time.Second},
	}
}

func (d *TrojanDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	serverAddr := net.JoinHostPort(d.node.Server, fmt.Sprintf("%d", d.node.Port))

	sni := d.node.SNI
	if sni == "" {
		sni = d.node.Server
	}

	tlsCfg := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: d.node.SkipCertVerify,
	}

	var rawConn net.Conn
	var err error

	if strings.ToLower(d.node.Network) == "ws" {
		// WebSocket transport
		wsURL := url.URL{
			Scheme: "wss",
			Host:   serverAddr,
			Path:   d.node.Path,
		}
		if wsURL.Path == "" {
			wsURL.Path = "/"
		}

		dialer := websocket.Dialer{
			TLSClientConfig:  tlsCfg,
			HandshakeTimeout: 10 * time.Second,
		}
		headers := http.Header{}
		if d.node.Host != "" {
			headers.Set("Host", d.node.Host)
		}

		ws, _, wsErr := dialer.DialContext(ctx, wsURL.String(), headers)
		if wsErr != nil {
			return nil, fmt.Errorf("trojan ws dial failed: %w", wsErr)
		}
		rawConn = NewWSConn(ws)
	} else {
		// Standard TCP + TLS
		rawConn, err = tls.DialWithDialer(&d.netDialer, "tcp", serverAddr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("trojan tls dial failed: %w", err)
		}
	}

	targetBytes, err := EncodeTargetAddr(target)
	if err != nil {
		rawConn.Close()
		return nil, err
	}

	// Trojan Request Packet:
	// 56-byte hex hash + "\r\n" + 0x01 (CONNECT) + targetAddr + "\r\n"
	req := make([]byte, 0, 56+2+1+len(targetBytes)+2)
	req = append(req, []byte(d.hexHash)...)
	req = append(req, '\r', '\n')
	req = append(req, 0x01) // CONNECT
	req = append(req, targetBytes...)
	req = append(req, '\r', '\n')

	if _, err := rawConn.Write(req); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("trojan handshake write failed: %w", err)
	}

	return rawConn, nil
}

// ==========================================
// Shadowsocks Dialer
// ==========================================
type ShadowsocksDialer struct {
	node      *model.Node
	cipher    core.Cipher
	netDialer net.Dialer
}

func NewShadowsocksDialer(n *model.Node) (*ShadowsocksDialer, error) {
	cipherMethod := strings.ToUpper(n.Cipher)
	// Normalize common cipher names
	switch strings.ToLower(cipherMethod) {
	case "aes-128-gcm":
		cipherMethod = "AEAD_AES_128_GCM"
	case "aes-256-gcm":
		cipherMethod = "AEAD_AES_256_GCM"
	case "chacha20-ietf-poly1305", "chacha20-poly1305":
		cipherMethod = "AEAD_CHACHA20_POLY1305"
	}

	ciph, err := core.PickCipher(cipherMethod, nil, n.Password)
	if err != nil {
		return nil, fmt.Errorf("unsupported shadowsocks cipher '%s': %w", n.Cipher, err)
	}

	return &ShadowsocksDialer{
		node:      n,
		cipher:    ciph,
		netDialer: net.Dialer{Timeout: 10 * time.Second},
	}, nil
}

func (d *ShadowsocksDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	serverAddr := net.JoinHostPort(d.node.Server, fmt.Sprintf("%d", d.node.Port))
	conn, err := d.netDialer.DialContext(ctx, "tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("shadowsocks dial failed: %w", err)
	}

	ssConn := d.cipher.StreamConn(conn)

	targetBytes, err := EncodeTargetAddr(target)
	if err != nil {
		ssConn.Close()
		return nil, err
	}

	if _, err := ssConn.Write(targetBytes); err != nil {
		ssConn.Close()
		return nil, fmt.Errorf("shadowsocks target write failed: %w", err)
	}

	return ssConn, nil
}

// ==========================================
// VLESS Dialer
// ==========================================
type VLESSDialer struct {
	node      *model.Node
	uuidBytes [16]byte
	netDialer net.Dialer
}

func NewVLESSDialer(n *model.Node) (*VLESSDialer, error) {
	parsedUUID, err := uuid.Parse(n.Password)
	if err != nil {
		return nil, fmt.Errorf("invalid vless uuid: %w", err)
	}

	return &VLESSDialer{
		node:      n,
		uuidBytes: parsedUUID,
		netDialer: net.Dialer{Timeout: 10 * time.Second},
	}, nil
}

func (d *VLESSDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	serverAddr := net.JoinHostPort(d.node.Server, fmt.Sprintf("%d", d.node.Port))

	var rawConn net.Conn
	var err error

	sni := d.node.SNI
	if sni == "" {
		sni = d.node.Server
	}

	tlsCfg := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: d.node.SkipCertVerify,
	}

	if strings.ToLower(d.node.Network) == "ws" {
		scheme := "ws"
		if d.node.TLS {
			scheme = "wss"
		}
		wsURL := url.URL{
			Scheme: scheme,
			Host:   serverAddr,
			Path:   d.node.Path,
		}
		if wsURL.Path == "" {
			wsURL.Path = "/"
		}

		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}
		if d.node.TLS {
			dialer.TLSClientConfig = tlsCfg
		}

		headers := http.Header{}
		if d.node.Host != "" {
			headers.Set("Host", d.node.Host)
		}

		ws, _, wsErr := dialer.DialContext(ctx, wsURL.String(), headers)
		if wsErr != nil {
			return nil, fmt.Errorf("vless ws dial failed: %w", wsErr)
		}
		rawConn = NewWSConn(ws)
	} else if d.node.TLS {
		rawConn, err = tls.DialWithDialer(&d.netDialer, "tcp", serverAddr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("vless tls dial failed: %w", err)
		}
	} else {
		rawConn, err = d.netDialer.DialContext(ctx, "tcp", serverAddr)
		if err != nil {
			return nil, fmt.Errorf("vless tcp dial failed: %w", err)
		}
	}

	targetBytes, err := EncodeTargetAddr(target)
	if err != nil {
		rawConn.Close()
		return nil, err
	}

	// VLESS Header:
	// [1 byte version (0x00)]
	// [16 bytes UUID]
	// [1 byte proto addons len (0x00)]
	// [1 byte command (0x01: TCP)]
	// [target address bytes (atyp + addr + port)]
	// Note: In VLESS spec, port comes before address or as part of socks5 addr.
	// Specifically:
	// [2 bytes port big-endian]
	// [1 byte atyp]
	// [target address bytes]
	// Let's decode port and atyp from targetBytes:
	// SOCKS5 atyp -> VLESS atyp mapping:
	// SOCKS5: 0x01 = IPv4, 0x03 = Domain, 0x04 = IPv6
	// VLESS:  0x01 = IPv4, 0x02 = Domain, 0x03 = IPv6
	vlessAtyp := byte(0x01)
	switch targetBytes[0] {
	case 0x01:
		vlessAtyp = 0x01
	case 0x03:
		vlessAtyp = 0x02 // Domain Name
	case 0x04:
		vlessAtyp = 0x03 // IPv6
	}

	portBytes := targetBytes[len(targetBytes)-2:]
	addrBytes := targetBytes[1 : len(targetBytes)-2]

	req := make([]byte, 0, 1+16+1+1+2+1+len(addrBytes))
	req = append(req, 0x00) // version
	req = append(req, d.uuidBytes[:]...)
	req = append(req, 0x00) // addons len
	req = append(req, 0x01) // command: TCP
	req = append(req, portBytes...)
	req = append(req, vlessAtyp)
	req = append(req, addrBytes...)

	if _, err := rawConn.Write(req); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("vless header write failed: %w", err)
	}

	return NewSmartVlessConn(rawConn), nil
}

// SmartVlessConn lazily detects and strips the VLESS response header on the first read
type SmartVlessConn struct {
	net.Conn
	reader      *bufio.Reader
	initialized bool
	mu          sync.Mutex
}

func NewSmartVlessConn(c net.Conn) *SmartVlessConn {
	return &SmartVlessConn{
		Conn:   c,
		reader: bufio.NewReader(c),
	}
}

func (c *SmartVlessConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	if !c.initialized {
		c.initialized = true
		c.mu.Unlock()

		firstByte, err := c.reader.ReadByte()
		if err != nil {
			return 0, err
		}

		if firstByte == 0x00 {
			// Version 0 VLESS response header: [addonsLen][addons...]
			addonsLenByte, err := c.reader.ReadByte()
			if err != nil {
				return 0, err
			}
			addonsLen := int(addonsLenByte)
			if addonsLen > 0 {
				discard := make([]byte, addonsLen)
				if _, err := io.ReadFull(c.reader, discard); err != nil {
					return 0, err
				}
			}
		} else {
			// No VLESS response header (direct stream like HTTP/TLS), unread byte
			_ = c.reader.UnreadByte()
		}
	} else {
		c.mu.Unlock()
	}

	return c.reader.Read(b)
}

// ==========================================
// VMess Dialer (WebSocket / TCP)
// ==========================================
type VMessDialer struct {
	node      *model.Node
	netDialer net.Dialer
}

func NewVMessDialer(n *model.Node) (*VMessDialer, error) {
	return &VMessDialer{
		node:      n,
		netDialer: net.Dialer{Timeout: 10 * time.Second},
	}, nil
}

func (d *VMessDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	serverAddr := net.JoinHostPort(d.node.Server, fmt.Sprintf("%d", d.node.Port))

	sni := d.node.SNI
	if sni == "" {
		sni = d.node.Server
	}
	tlsCfg := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: d.node.SkipCertVerify,
	}

	var rawConn net.Conn
	var err error

	if strings.ToLower(d.node.Network) == "ws" {
		scheme := "ws"
		if d.node.TLS {
			scheme = "wss"
		}
		wsURL := url.URL{
			Scheme: scheme,
			Host:   serverAddr,
			Path:   d.node.Path,
		}
		if wsURL.Path == "" {
			wsURL.Path = "/"
		}

		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}
		if d.node.TLS {
			dialer.TLSClientConfig = tlsCfg
		}

		headers := http.Header{}
		if d.node.Host != "" {
			headers.Set("Host", d.node.Host)
		}

		ws, _, wsErr := dialer.DialContext(ctx, wsURL.String(), headers)
		if wsErr != nil {
			return nil, fmt.Errorf("vmess ws dial failed: %w", wsErr)
		}
		rawConn = NewWSConn(ws)
	} else if d.node.TLS {
		rawConn, err = tls.DialWithDialer(&d.netDialer, "tcp", serverAddr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("vmess tls dial failed: %w", err)
		}
	} else {
		rawConn, err = d.netDialer.DialContext(ctx, "tcp", serverAddr)
		if err != nil {
			return nil, fmt.Errorf("vmess tcp dial failed: %w", err)
		}
	}

	client, err := vmess.NewClient(d.node.Password, d.node.Cipher, d.node.AlterID)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("vmess client init failed: %w", err)
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	p, _ := strconv.Atoi(portStr)
	dest := M.ParseSocksaddrHostPort(host, uint16(p))

	return client.DialConn(rawConn, dest)
}

// ==========================================
// SOCKS5 Outbound Dialer
// ==========================================
type Socks5Dialer struct {
	dialer proxy.Dialer
}

func NewSocks5Dialer(n *model.Node) (*Socks5Dialer, error) {
	serverAddr := net.JoinHostPort(n.Server, fmt.Sprintf("%d", n.Port))
	var auth *proxy.Auth
	if n.Password != "" || n.Cipher != "" {
		auth = &proxy.Auth{
			User:     n.Cipher, // Username stored in Cipher
			Password: n.Password,
		}
	}

	d, err := proxy.SOCKS5("tcp", serverAddr, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("failed to create socks5 dialer: %w", err)
	}

	return &Socks5Dialer{dialer: d}, nil
}

func (d *Socks5Dialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	if ctxDialer, ok := d.dialer.(proxy.ContextDialer); ok {
		return ctxDialer.DialContext(ctx, network, target)
	}
	return d.dialer.Dial(network, target)
}

// ==========================================
// HTTP CONNECT Outbound Dialer
// ==========================================
type HTTPDialer struct {
	node      *model.Node
	netDialer net.Dialer
}

func NewHTTPDialer(n *model.Node) *HTTPDialer {
	return &HTTPDialer{
		node:      n,
		netDialer: net.Dialer{Timeout: 10 * time.Second},
	}
}

func (d *HTTPDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	serverAddr := net.JoinHostPort(d.node.Server, fmt.Sprintf("%d", d.node.Port))
	conn, err := d.netDialer.DialContext(ctx, "tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("http proxy dial failed: %w", err)
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if d.node.Password != "" || d.node.Cipher != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", d.node.Cipher, d.node.Password)))
		req += fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth)
	}
	req += "Proxy-Connection: Keep-Alive\r\n\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("http proxy connect write failed: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("http proxy response read failed: %w", err)
	}
	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("http proxy returned non-200: %s", resp.Status)
	}

	return conn, nil
}
