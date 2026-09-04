package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"cyberproxypool/pkg/model"
)

type StoreData struct {
	Config        model.AppConfig                `json:"config"`
	Subscriptions map[string]*model.Subscription `json:"subscriptions"`
	Nodes         map[string]*model.Node         `json:"nodes"`
}

// Store handles thread-safe persistence to disk
type Store struct {
	mu       sync.RWMutex
	filePath string
	data     StoreData
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}

	filePath := filepath.Join(dataDir, "proxypool.json")
	s := &Store{
		filePath: filePath,
		data: StoreData{
			Config:        model.DefaultAppConfig(),
			Subscriptions: make(map[string]*model.Subscription),
			Nodes:         make(map[string]*model.Node),
		},
	}

	if err := s.load(); err != nil {
		// If file doesn't exist, create it with default config
		if os.IsNotExist(err) {
			if saveErr := s.saveLocked(); saveErr != nil {
				return nil, saveErr
			}
		} else {
			return nil, fmt.Errorf("failed to load storage file: %w", err)
		}
	}

	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var data StoreData
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}

	if data.Subscriptions == nil {
		data.Subscriptions = make(map[string]*model.Subscription)
	}
	if data.Nodes == nil {
		data.Nodes = make(map[string]*model.Node)
	}
	if data.Config.WebPort == 0 {
		data.Config = model.DefaultAppConfig()
	}

	s.data = data
	return nil
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, b, 0644); err != nil {
		return err
	}

	return os.Rename(tmpFile, s.filePath)
}

// Subscriptions

func (s *Store) SaveSubscription(sub *model.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	sub.UpdatedAt = time.Now()

	s.data.Subscriptions[sub.ID] = sub
	return s.saveLocked()
}

func (s *Store) GetSubscription(id string) (*model.Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sub, ok := s.data.Subscriptions[id]
	if !ok {
		return nil, false
	}
	// return a copy
	cp := *sub
	return &cp, true
}

func (s *Store) ListSubscriptions() []*model.Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*model.Subscription, 0, len(s.data.Subscriptions))
	for _, sub := range s.data.Subscriptions {
		cp := *sub
		list = append(list, &cp)
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].ID < list[j].ID
	})

	return list
}

func (s *Store) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data.Subscriptions, id)

	// Delete corresponding nodes
	for nodeID, node := range s.data.Nodes {
		if node.SubID == id {
			delete(s.data.Nodes, nodeID)
		}
	}

	return s.saveLocked()
}

// Nodes

func (s *Store) UpsertNodes(nodes []model.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, n := range nodes {
		nodeCopy := n
		// If node already exists, preserve runtime fields like latency/testedAt
		if existing, ok := s.data.Nodes[n.ID]; ok {
			nodeCopy.Latency = existing.Latency
			nodeCopy.ExitIP = existing.ExitIP
			nodeCopy.LastTestedAt = existing.LastTestedAt
			nodeCopy.InboundPort = existing.InboundPort
			nodeCopy.IsRunning = existing.IsRunning
		}
		s.data.Nodes[n.ID] = &nodeCopy
	}

	return s.saveLocked()
}

func (s *Store) ReplaceNodesForSubscription(subID string, nodes []model.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old nodes for this subID
	for nodeID, node := range s.data.Nodes {
		if node.SubID == subID {
			delete(s.data.Nodes, nodeID)
		}
	}

	// Insert new
	for _, n := range nodes {
		nodeCopy := n
		s.data.Nodes[n.ID] = &nodeCopy
	}

	if sub, ok := s.data.Subscriptions[subID]; ok {
		sub.NodeCount = len(nodes)
		sub.UpdatedAt = time.Now()
	}

	return s.saveLocked()
}

func (s *Store) GetNode(id string) (*model.Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, ok := s.data.Nodes[id]
	if !ok {
		return nil, false
	}
	cp := *node
	return &cp, true
}

func (s *Store) ListNodes() []*model.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*model.Node, 0, len(s.data.Nodes))
	for _, node := range s.data.Nodes {
		cp := *node
		list = append(list, &cp)
	}

	// Deterministic standard sort:
	// 1. Subscription Name / ID
	// 2. Node Name
	// 3. Node ID
	sort.Slice(list, func(i, j int) bool {
		if list[i].SubName != list[j].SubName {
			return list[i].SubName < list[j].SubName
		}
		if list[i].SubID != list[j].SubID {
			return list[i].SubID < list[j].SubID
		}
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].ID < list[j].ID
	})

	return list
}

func (s *Store) UpdateNodeRuntime(id string, isRunning bool, port int, latency int64, exitIP string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node, ok := s.data.Nodes[id]; ok {
		node.IsRunning = isRunning
		node.InboundPort = port
		if latency != 0 {
			node.Latency = latency
		}
		if exitIP != "" {
			node.ExitIP = exitIP
		}
		node.ErrorMessage = errMsg
		if latency > 0 {
			node.LastTestedAt = time.Now()
		}
	}
}

// Config

func (s *Store) GetConfig() model.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Config
}

func (s *Store) UpdateConfig(cfg model.AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Config = cfg
	return s.saveLocked()
}
