package model

import (
	"sync"
	"time"
)

// ProxyProtocol defines supported proxy protocols
type ProxyProtocol string

const (
	ProtoTrojan      ProxyProtocol = "trojan"
	ProtoShadowsocks ProxyProtocol = "ss"
	ProtoVMess       ProxyProtocol = "vmess"
	ProtoVLESS       ProxyProtocol = "vless"
	ProtoSocks5      ProxyProtocol = "socks5"
	ProtoHTTP        ProxyProtocol = "http"
)

// RoutingStrategy defines load balancing algorithms for tunnel pool
type RoutingStrategy string

const (
	StrategyRoundRobin   RoutingStrategy = "round-robin"
	StrategyRandom       RoutingStrategy = "random"
	StrategyLeastLatency RoutingStrategy = "least-latency"
)

// Subscription represents a proxy subscription source
type Subscription struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	RawContent  string    `json:"raw_content,omitempty"`
	Format      string    `json:"format"` // "clash", "base64", "links"
	NodeCount   int       `json:"node_count"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastError   string    `json:"last_error,omitempty"`
}

// Node represents a proxy node definition and its runtime state
type Node struct {
	ID             string        `json:"id"`
	SubID          string        `json:"sub_id"`
	SubName        string        `json:"sub_name"`
	Name           string        `json:"name"`
	Protocol       ProxyProtocol `json:"protocol"`
	Server         string        `json:"server"`
	Port           int           `json:"port"`
	Password       string        `json:"password,omitempty"` // Password for trojan/ss, or UUID for vmess/vless
	Cipher         string        `json:"cipher,omitempty"`   // Cipher method for ss/vmess
	Network        string        `json:"network,omitempty"`  // "tcp", "ws"
	Path           string        `json:"path,omitempty"`     // WebSocket path
	Host           string        `json:"host,omitempty"`     // WebSocket Host header
	SNI            string        `json:"sni,omitempty"`      // TLS ServerName
	TLS            bool          `json:"tls"`
	SkipCertVerify bool          `json:"skip_cert_verify"`
	AlterID        int           `json:"alter_id,omitempty"` // For VMess

	// Runtime fields
	InboundPort   int       `json:"inbound_port"`
	IsRunning     bool      `json:"is_running"`
	Latency       int64     `json:"latency"` // in milliseconds, -1 if unreachable
	ExitIP        string    `json:"exit_ip,omitempty"`
	Country       string    `json:"country,omitempty"`
	Flag          string    `json:"flag,omitempty"`
	LastTestedAt  time.Time `json:"last_tested_at,omitempty"`
	UploadBytes   int64     `json:"upload_bytes"`
	DownloadBytes int64     `json:"download_bytes"`
	ActiveConns   int64     `json:"active_conns"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}

// TunnelConfig defines configuration for unified aggregate proxy pool
type TunnelConfig struct {
	Enabled  bool            `json:"enabled"`
	Port     int             `json:"port"`
	Strategy RoutingStrategy `json:"strategy"`
}

// AppConfig represents persisted application settings
type AppConfig struct {
	WebPort         int          `json:"web_port"`
	WebHost         string       `json:"web_host"`
	BaseInboundPort int          `json:"base_inbound_port"`
	Tunnel          TunnelConfig `json:"tunnel"`
	TestURL         string       `json:"test_url"`
	TestTimeoutSec  int          `json:"test_timeout_sec"`
}

// DefaultAppConfig returns sane defaults
func DefaultAppConfig() AppConfig {
	return AppConfig{
		WebPort:         8088,
		WebHost:         "0.0.0.0",
		BaseInboundPort: 20001,
		Tunnel: TunnelConfig{
			Enabled:  true,
			Port:     10808,
			Strategy: StrategyRoundRobin,
		},
		TestURL:        "https://api.ipify.org?format=json",
		TestTimeoutSec: 8,
	}
}

// SystemStatus provides runtime dashboard metrics
type SystemStatus struct {
	Version         string    `json:"version"`
	Uptime          string    `json:"uptime"`
	TotalNodes      int       `json:"total_nodes"`
	RunningNodes    int       `json:"running_nodes"`
	Subscriptions   int       `json:"subscriptions"`
	TunnelRunning   bool      `json:"tunnel_running"`
	TunnelPort      int       `json:"tunnel_port"`
	TunnelStrategy  string    `json:"tunnel_strategy"`
	BaseInboundPort int       `json:"base_inbound_port"`
	MemoryAllocMB   float64   `json:"memory_alloc_mb"`
	Goroutines      int       `json:"goroutines"`
	StartedAt       time.Time `json:"started_at"`
}

// Thread-safe node stats helper
type NodeStats struct {
	mu            sync.Mutex
	UploadBytes   int64
	DownloadBytes int64
	ActiveConns   int64
}

func (s *NodeStats) AddUpload(n int64) {
	s.mu.Lock()
	s.UploadBytes += n
	s.mu.Unlock()
}

func (s *NodeStats) AddDownload(n int64) {
	s.mu.Lock()
	s.DownloadBytes += n
	s.mu.Unlock()
}

func (s *NodeStats) IncrConn() {
	s.mu.Lock()
	s.ActiveConns++
	s.mu.Unlock()
}

func (s *NodeStats) DecrConn() {
	s.mu.Lock()
	if s.ActiveConns > 0 {
		s.ActiveConns--
	}
	s.mu.Unlock()
}

func (s *NodeStats) Get() (int64, int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.UploadBytes, s.DownloadBytes, s.ActiveConns
}
