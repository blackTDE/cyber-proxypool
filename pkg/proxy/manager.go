package proxy

import (
	"fmt"
	"net"
	"sync"

	"cyberproxypool/pkg/dialer"
	"cyberproxypool/pkg/model"
)

type nodeRuntime struct {
	node     *model.Node
	dialer   dialer.OutboundDialer
	inbound  *InboundListener
	stats    *model.NodeStats
	port     int
}

// Manager coordinates all individual proxy listeners and updates the tunnel pool
type Manager struct {
	mu           sync.RWMutex
	host         string
	basePort     int
	usedPorts    map[int]string      // port -> nodeID
	runningNodes map[string]*nodeRuntime // nodeID -> runtime
	nodeStats    map[string]*model.NodeStats
	tunnel       *TunnelPool
}

func NewManager(host string, basePort int, tunnel *TunnelPool) *Manager {
	return &Manager{
		host:         host,
		basePort:     basePort,
		usedPorts:    make(map[int]string),
		runningNodes: make(map[string]*nodeRuntime),
		nodeStats:    make(map[string]*model.NodeStats),
		tunnel:       tunnel,
	}
}

// SetBasePort updates the starting port for inbound listeners
func (m *Manager) SetBasePort(basePort int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.basePort = basePort
}

// GetBasePort returns the current starting port
func (m *Manager) GetBasePort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.basePort
}

// StartNode creates and launches an inbound listener for a specific node
func (m *Manager) StartNode(node *model.Node) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already running, return current port
	if rt, exists := m.runningNodes[node.ID]; exists {
		return rt.port, nil
	}

	// Create outbound dialer
	outbound, err := dialer.NewDialerFromNode(node)
	if err != nil {
		return 0, fmt.Errorf("failed to create outbound dialer: %w", err)
	}

	// Get or create stats tracker
	stats, exists := m.nodeStats[node.ID]
	if !exists {
		stats = &model.NodeStats{}
		m.nodeStats[node.ID] = stats
	}

	// Allocate next available port
	port := m.allocatePort(node.ID)
	addr := net.JoinHostPort(m.host, fmt.Sprintf("%d", port))

	provider := NewFixedDialerProvider(outbound, stats)
	inbound, err := NewInboundListener(addr, provider)
	if err != nil {
		delete(m.usedPorts, port)
		return 0, fmt.Errorf("failed to bind inbound listener on %s: %w", addr, err)
	}

	rt := &nodeRuntime{
		node:    node,
		dialer:  outbound,
		inbound: inbound,
		stats:   stats,
		port:    port,
	}

	m.runningNodes[node.ID] = rt
	node.IsRunning = true
	node.InboundPort = port
	node.ErrorMessage = ""

	m.syncTunnelLocked()
	return port, nil
}

// StopNode stops the inbound listener for a specific node
func (m *Manager) StopNode(nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, exists := m.runningNodes[nodeID]
	if !exists {
		return nil
	}

	err := rt.inbound.Close()
	delete(m.usedPorts, rt.port)
	delete(m.runningNodes, nodeID)
	rt.node.IsRunning = false
	rt.node.InboundPort = 0

	m.syncTunnelLocked()
	return err
}

// StartAll launches inbound listeners for all provided nodes
func (m *Manager) StartAll(nodes []*model.Node) (int, error) {
	successCount := 0
	var lastErr error

	for _, n := range nodes {
		_, err := m.StartNode(n)
		if err != nil {
			lastErr = err
			n.ErrorMessage = err.Error()
		} else {
			successCount++
		}
	}

	if successCount == 0 && lastErr != nil {
		return 0, lastErr
	}
	return successCount, nil
}

// StopAll stops all currently running node inbound listeners
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, rt := range m.runningNodes {
		if err := rt.inbound.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		rt.node.IsRunning = false
		rt.node.InboundPort = 0
		delete(m.usedPorts, rt.port)
		delete(m.runningNodes, id)
	}

	m.syncTunnelLocked()
	return firstErr
}

// IsNodeRunning checks if a node is running
func (m *Manager) IsNodeRunning(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.runningNodes[nodeID]
	return exists
}

// GetNodePort returns the assigned inbound port or 0 if not running
func (m *Manager) GetNodePort(nodeID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt, exists := m.runningNodes[nodeID]; exists {
		return rt.port
	}
	return 0
}

// GetRunningCount returns total number of active listeners
func (m *Manager) GetRunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.runningNodes)
}

// GetStats returns current transfer metrics for a node
func (m *Manager) GetStats(nodeID string) (int64, int64, int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.nodeStats[nodeID]; ok {
		return s.Get()
	}
	return 0, 0, 0
}

func (m *Manager) allocatePort(nodeID string) int {
	port := m.basePort
	for {
		if _, taken := m.usedPorts[port]; !taken {
			m.usedPorts[port] = nodeID
			return port
		}
		port++
	}
}

func (m *Manager) syncTunnelLocked() {
	if m.tunnel == nil {
		return
	}

	entries := make([]*ActiveNodeEntry, 0, len(m.runningNodes))
	for _, rt := range m.runningNodes {
		entries = append(entries, &ActiveNodeEntry{
			Node:   rt.node,
			Dialer: rt.dialer,
			Stats:  rt.stats,
		})
	}
	m.tunnel.UpdateActiveNodes(entries)
}
