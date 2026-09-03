package proxy

import (
	"testing"

	"cyberproxypool/pkg/model"
)

func TestTunnelPoolRouting(t *testing.T) {
	tunnelCfg := model.TunnelConfig{
		Enabled:  true,
		Port:     10808,
		Strategy: model.StrategyRoundRobin,
	}

	tp := NewTunnelPool(tunnelCfg, "127.0.0.1")

	node1 := &model.Node{ID: "n1", Name: "Node 1", Latency: 100}
	node2 := &model.Node{ID: "n2", Name: "Node 2", Latency: 50}
	node3 := &model.Node{ID: "n3", Name: "Node 3", Latency: 200}

	entries := []*ActiveNodeEntry{
		{Node: node1, Dialer: &mockDirectDialer{}},
		{Node: node2, Dialer: &mockDirectDialer{}},
		{Node: node3, Dialer: &mockDirectDialer{}},
	}
	tp.UpdateActiveNodes(entries)

	// 1. Test Round Robin
	tp.UpdateConfig(model.TunnelConfig{Enabled: true, Port: 10808, Strategy: model.StrategyRoundRobin})
	_, _, err := tp.GetDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2. Test Least Latency (should pick node2 which has 50ms)
	tp.UpdateConfig(model.TunnelConfig{Enabled: true, Port: 10808, Strategy: model.StrategyLeastLatency})
	d, _, err := tp.GetDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatalf("expected dialer")
	}

	// 3. Test Random
	tp.UpdateConfig(model.TunnelConfig{Enabled: true, Port: 10808, Strategy: model.StrategyRandom})
	_, _, err = tp.GetDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerPortAllocation(t *testing.T) {
	mgr := NewManager("127.0.0.1", 20000, nil)

	p1 := mgr.allocatePort("n1")
	if p1 != 20000 {
		t.Errorf("expected port 20000, got %d", p1)
	}

	p2 := mgr.allocatePort("n2")
	if p2 != 20001 {
		t.Errorf("expected port 20001, got %d", p2)
	}
}
