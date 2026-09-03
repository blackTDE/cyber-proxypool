package storage

import (
	"os"
	"testing"

	"cyberproxypool/pkg/model"
)

func TestStorePersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cyberproxy-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	// 1. Add subscription
	sub := &model.Subscription{
		ID:      "sub-abc",
		Name:    "Test Subscription",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	if err := store.SaveSubscription(sub); err != nil {
		t.Fatalf("failed to save sub: %v", err)
	}

	// 2. Add nodes
	nodes := []model.Node{
		{
			ID:       "node-1",
			SubID:    "sub-abc",
			Name:     "Node 1",
			Protocol: model.ProtoTrojan,
			Server:   "hk.example.com",
			Port:     443,
		},
		{
			ID:       "node-2",
			SubID:    "sub-abc",
			Name:     "Node 2",
			Protocol: model.ProtoShadowsocks,
			Server:   "jp.example.com",
			Port:     8388,
		},
	}
	if err := store.ReplaceNodesForSubscription("sub-abc", nodes); err != nil {
		t.Fatalf("failed to replace nodes: %v", err)
	}

	// 3. Re-open store from disk and verify
	reopened, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}

	subLoaded, ok := reopened.GetSubscription("sub-abc")
	if !ok || subLoaded.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %v", subLoaded)
	}

	nodesLoaded := reopened.ListNodes()
	if len(nodesLoaded) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodesLoaded))
	}
}
