package proxy

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"

	"cyberproxypool/pkg/dialer"
	"cyberproxypool/pkg/model"
)

type ActiveNodeEntry struct {
	Node   *model.Node
	Dialer dialer.OutboundDialer
	Stats  *model.NodeStats
}

// TunnelPool manages an aggregated rotating proxy port that balances across all active nodes
type TunnelPool struct {
	mu          sync.RWMutex
	config      model.TunnelConfig
	host        string
	activeNodes []*ActiveNodeEntry
	rrIndex     uint64
	inbound     *InboundListener
	rng         *rand.Rand
	rngMu       sync.Mutex
	stats       model.NodeStats
}

func NewTunnelPool(cfg model.TunnelConfig, host string) *TunnelPool {
	return &TunnelPool{
		config: cfg,
		host:   host,
		rng:    rand.New(rand.NewSource(42)),
	}
}

// UpdateActiveNodes synchronizes the pool with currently running nodes
func (t *TunnelPool) UpdateActiveNodes(entries []*ActiveNodeEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.activeNodes = entries
}

// GetDialer selects an outbound dialer according to the configured routing strategy
func (t *TunnelPool) GetDialer() (dialer.OutboundDialer, *model.NodeStats, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	n := len(t.activeNodes)
	if n == 0 {
		return nil, nil, errors.New("no active nodes available in tunnel pool")
	}

	var selected *ActiveNodeEntry

	switch t.config.Strategy {
	case model.StrategyRandom:
		t.rngMu.Lock()
		idx := t.rng.Intn(n)
		t.rngMu.Unlock()
		selected = t.activeNodes[idx]

	case model.StrategyLeastLatency:
		var best *ActiveNodeEntry
		var minLat int64 = 999999999
		for _, entry := range t.activeNodes {
			if entry.Node.Latency > 0 && entry.Node.Latency < minLat {
				minLat = entry.Node.Latency
				best = entry
			}
		}
		if best != nil {
			selected = best
		} else {
			// Fallback to round-robin if no node has valid latency yet
			idx := atomic.AddUint64(&t.rrIndex, 1) % uint64(n)
			selected = t.activeNodes[idx]
		}

	case model.StrategyRoundRobin:
		fallthrough
	default:
		idx := atomic.AddUint64(&t.rrIndex, 1) % uint64(n)
		selected = t.activeNodes[idx]
	}

	return selected.Dialer, selected.Stats, nil
}

// Start launches the tunnel inbound listener
func (t *TunnelPool) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.inbound != nil {
		return nil // already running
	}

	addr := net.JoinHostPort(t.host, fmt.Sprintf("%d", t.config.Port))
	inbound, err := NewInboundListener(addr, t)
	if err != nil {
		return fmt.Errorf("failed to start tunnel listener: %w", err)
	}

	t.inbound = inbound
	t.config.Enabled = true
	return nil
}

// Stop terminates the tunnel inbound listener
func (t *TunnelPool) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.inbound == nil {
		return nil
	}

	err := t.inbound.Close()
	t.inbound = nil
	t.config.Enabled = false
	return err
}

// IsRunning returns whether the tunnel listener is active
func (t *TunnelPool) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.inbound != nil
}

// Config returns the current tunnel configuration
func (t *TunnelPool) Config() model.TunnelConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config
}

// UpdateConfig updates port and strategy, restarting if port changed
func (t *TunnelPool) UpdateConfig(cfg model.TunnelConfig) error {
	t.mu.Lock()
	portChanged := t.config.Port != cfg.Port
	wasRunning := t.inbound != nil
	t.config = cfg
	t.mu.Unlock()

	if wasRunning && (portChanged || !cfg.Enabled) {
		if err := t.Stop(); err != nil {
			return err
		}
	}

	if cfg.Enabled && (portChanged || !wasRunning) {
		return t.Start()
	}

	return nil
}
