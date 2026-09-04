package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"cyberproxypool/pkg/model"
	"cyberproxypool/pkg/parser"
	"cyberproxypool/pkg/proxy"
	"cyberproxypool/pkg/storage"
	"cyberproxypool/pkg/tester"
	"github.com/google/uuid"
)

type Server struct {
	store     *storage.Store
	manager   *proxy.Manager
	tunnel    *proxy.TunnelPool
	tester    *tester.Tester
	startedAt time.Time

	// SSE Clients
	sseClients map[chan string]bool
	sseMu      sync.RWMutex
}

func NewServer(store *storage.Store, mgr *proxy.Manager, tunnel *proxy.TunnelPool, t *tester.Tester) *Server {
	return &Server{
		store:      store,
		manager:    mgr,
		tunnel:     tunnel,
		tester:     t,
		startedAt:  time.Now(),
		sseClients: make(map[chan string]bool),
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/subscriptions", s.handleSubscriptions)
	mux.HandleFunc("/api/subscriptions/", s.handleSubscriptionByID)
	mux.HandleFunc("/api/nodes", s.handleNodes)
	mux.HandleFunc("/api/nodes/", s.handleNodeAction)
	mux.HandleFunc("/api/tunnel", s.handleTunnel)
	mux.HandleFunc("/api/events", s.handleSSE)
}

// BroadcastSSE sends event to all connected dashboard clients
func (s *Server) BroadcastSSE(eventType string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(payload))

	s.sseMu.RLock()
	defer s.sseMu.RUnlock()

	for ch := range s.sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	msgChan := make(chan string, 16)
	s.sseMu.Lock()
	s.sseClients[msgChan] = true
	s.sseMu.Unlock()

	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, msgChan)
		s.sseMu.Unlock()
	}()

	notify := r.Context().Done()
	// Send initial ping
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"ok\"}\n\n")
	flusher.Flush()

	for {
		select {
		case <-notify:
			return
		case msg := <-msgChan:
			fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	subs := s.store.ListSubscriptions()
	nodes := s.store.ListNodes()

	uptime := time.Since(s.startedAt).Round(time.Second).String()

	status := model.SystemStatus{
		Version:         "1.0.0",
		Uptime:          uptime,
		TotalNodes:      len(nodes),
		RunningNodes:    s.manager.GetRunningCount(),
		Subscriptions:   len(subs),
		TunnelRunning:   s.tunnel.IsRunning(),
		TunnelPort:      s.tunnel.Config().Port,
		TunnelStrategy:  string(s.tunnel.Config().Strategy),
		BaseInboundPort: s.manager.GetBasePort(),
		MemoryAllocMB:   float64(m.Alloc) / 1024 / 1024,
		Goroutines:      runtime.NumGoroutine(),
		StartedAt:       s.startedAt,
	}

	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		return
	}

	cfg := s.store.GetConfig()
	cfg.BaseInboundPort = s.manager.GetBasePort()

	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, cfg)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var req struct {
			BaseInboundPort int    `json:"base_inbound_port"`
			TestURL         string `json:"test_url"`
			TestTimeoutSec  int    `json:"test_timeout_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request json"})
			return
		}

		if req.BaseInboundPort > 0 {
			if req.BaseInboundPort < 1024 || req.BaseInboundPort > 65000 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "base_inbound_port must be between 1024 and 65000"})
				return
			}
			cfg.BaseInboundPort = req.BaseInboundPort
			s.manager.SetBasePort(req.BaseInboundPort)
		}

		if req.TestURL != "" {
			cfg.TestURL = strings.TrimSpace(req.TestURL)
		}
		if req.TestTimeoutSec > 0 {
			cfg.TestTimeoutSec = req.TestTimeoutSec
		}

		if err := s.store.UpdateConfig(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save config: %s", err.Error())})
			return
		}

		s.BroadcastSSE("config_updated", cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"config":  cfg,
		})
		return
	}

	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		subs := s.store.ListSubscriptions()
		writeJSON(w, http.StatusOK, subs)

	case http.MethodPost:
		var req struct {
			Name    string `json:"name"`
			URL     string `json:"url"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		rawContent := req.Content
		if req.URL != "" && rawContent == "" {
			fetched, err := parser.FetchSubscription(req.URL, 15*time.Second)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to fetch subscription: %v", err))
				return
			}
			rawContent = fetched
		}

		if rawContent == "" {
			writeError(w, http.StatusBadRequest, "must provide either subscription URL or content")
			return
		}

		subID := "sub-" + uuid.New().String()[:8]
		subName := req.Name
		if subName == "" {
			if req.URL != "" {
				subName = "Sub " + subID
			} else {
				subName = "Custom Nodes " + subID
			}
		}

		nodes, format, err := parser.ParseContent(rawContent, subID, subName)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse nodes: %v", err))
			return
		}

		sub := &model.Subscription{
			ID:         subID,
			Name:       subName,
			URL:        req.URL,
			Format:     format,
			NodeCount:  len(nodes),
			Enabled:    true,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := s.store.SaveSubscription(sub); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if err := s.store.ReplaceNodesForSubscription(subID, nodes); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.BroadcastSSE("subscription_added", sub)
		writeJSON(w, http.StatusCreated, map[string]any{
			"subscription": sub,
			"node_count":   len(nodes),
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSubscriptionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/subscriptions/")
	parts := strings.Split(path, "/")
	id := parts[0]

	if len(parts) == 1 && r.Method == http.MethodDelete {
		// Stop any running nodes of this subscription
		nodes := s.store.ListNodes()
		for _, n := range nodes {
			if n.SubID == id && s.manager.IsNodeRunning(n.ID) {
				s.manager.StopNode(n.ID)
			}
		}

		if err := s.store.DeleteSubscription(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.BroadcastSSE("subscription_deleted", map[string]string{"id": id})
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	if len(parts) == 2 && parts[1] == "refresh" && r.Method == http.MethodPost {
		sub, ok := s.store.GetSubscription(id)
		if !ok {
			writeError(w, http.StatusNotFound, "subscription not found")
			return
		}
		if sub.URL == "" {
			writeError(w, http.StatusBadRequest, "cannot refresh subscription without URL")
			return
		}

		fetched, err := parser.FetchSubscription(sub.URL, 15*time.Second)
		if err != nil {
			sub.LastError = err.Error()
			s.store.SaveSubscription(sub)
			writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to fetch: %v", err))
			return
		}

		nodes, format, err := parser.ParseContent(fetched, sub.ID, sub.Name)
		if err != nil {
			sub.LastError = err.Error()
			s.store.SaveSubscription(sub)
			writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse: %v", err))
			return
		}

		sub.Format = format
		sub.NodeCount = len(nodes)
		sub.LastError = ""
		s.store.SaveSubscription(sub)
		s.store.ReplaceNodesForSubscription(sub.ID, nodes)

		s.BroadcastSSE("subscription_refreshed", sub)
		writeJSON(w, http.StatusOK, map[string]any{
			"subscription": sub,
			"node_count":   len(nodes),
		})
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := s.store.ListNodes()
	subIDFilter := r.URL.Query().Get("sub_id")
	searchFilter := strings.ToLower(r.URL.Query().Get("search"))
	runningOnly := r.URL.Query().Get("running_only") == "true"

	var filtered []*model.Node
	for _, n := range nodes {
		// Sync live runtime state
		n.IsRunning = s.manager.IsNodeRunning(n.ID)
		n.InboundPort = s.manager.GetNodePort(n.ID)
		up, down, conns := s.manager.GetStats(n.ID)
		n.UploadBytes = up
		n.DownloadBytes = down
		n.ActiveConns = conns

		if subIDFilter != "" && n.SubID != subIDFilter {
			continue
		}
		if runningOnly && !n.IsRunning {
			continue
		}
		if searchFilter != "" {
			if !strings.Contains(strings.ToLower(n.Name), searchFilter) &&
				!strings.Contains(strings.ToLower(n.Server), searchFilter) &&
				!strings.Contains(strings.ToLower(n.Country), searchFilter) {
				continue
			}
		}

		filtered = append(filtered, n)
	}

	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleNodeAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.Split(path, "/")

	// Batch actions: /api/nodes/start-all, /api/nodes/stop-all, /api/nodes/test-all
	if len(parts) == 1 {
		action := parts[0]
		switch action {
		case "start-all":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			nodes := s.store.ListNodes()
			count, err := s.manager.StartAll(nodes)
			for _, n := range nodes {
				s.store.UpdateNodeRuntime(n.ID, n.IsRunning, n.InboundPort, n.Latency, n.ExitIP, n.ErrorMessage)
			}
			s.BroadcastSSE("nodes_updated", map[string]any{"action": "start-all", "count": count})
			writeJSON(w, http.StatusOK, map[string]any{"started": count, "error": fmt.Sprint(err)})
			return

		case "stop-all":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.manager.StopAll()
			nodes := s.store.ListNodes()
			for _, n := range nodes {
				s.store.UpdateNodeRuntime(n.ID, false, 0, n.Latency, n.ExitIP, "")
			}
			s.BroadcastSSE("nodes_updated", map[string]any{"action": "stop-all"})
			writeJSON(w, http.StatusOK, map[string]string{"status": "all stopped"})
			return

		case "test-all":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			go func() {
				nodes := s.store.ListNodes()
				cfg := s.store.GetConfig()
				s.tester.TestAll(context.Background(), nodes, cfg.TestURL, 15, func(res tester.TestResult) {
					s.store.UpdateNodeRuntime(res.NodeID, s.manager.IsNodeRunning(res.NodeID), s.manager.GetNodePort(res.NodeID), res.Latency, res.ExitIP, res.Error)
					s.BroadcastSSE("node_tested", res)
				})
				s.BroadcastSSE("test_complete", map[string]string{"status": "complete"})
			}()
			writeJSON(w, http.StatusOK, map[string]string{"status": "testing initiated"})
			return
		}
	}

	// Single node actions: /api/nodes/:id/start, /api/nodes/:id/stop, /api/nodes/:id/test
	if len(parts) == 2 {
		nodeID := parts[0]
		action := parts[1]

		node, ok := s.store.GetNode(nodeID)
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		switch action {
		case "start":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			port, err := s.manager.StartNode(node)
			if err != nil {
				s.store.UpdateNodeRuntime(node.ID, false, 0, node.Latency, node.ExitIP, err.Error())
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start listener: %v", err))
				return
			}
			s.store.UpdateNodeRuntime(node.ID, true, port, node.Latency, node.ExitIP, "")
			s.BroadcastSSE("node_started", map[string]any{"id": node.ID, "port": port})
			writeJSON(w, http.StatusOK, map[string]any{"status": "running", "port": port})
			return

		case "stop":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := s.manager.StopNode(node.ID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.store.UpdateNodeRuntime(node.ID, false, 0, node.Latency, node.ExitIP, "")
			s.BroadcastSSE("node_stopped", map[string]string{"id": node.ID})
			writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
			return

		case "test":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			cfg := s.store.GetConfig()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			res := s.tester.TestNode(ctx, node, cfg.TestURL)
			s.store.UpdateNodeRuntime(node.ID, s.manager.IsNodeRunning(node.ID), s.manager.GetNodePort(node.ID), res.Latency, res.ExitIP, res.Error)
			s.BroadcastSSE("node_tested", res)
			writeJSON(w, http.StatusOK, res)
			return
		}
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.tunnel.Config()
		status := map[string]any{
			"enabled":     cfg.Enabled,
			"port":        cfg.Port,
			"strategy":    cfg.Strategy,
			"is_running":  s.tunnel.IsRunning(),
			"pool_listen": fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		}
		writeJSON(w, http.StatusOK, status)

	case http.MethodPost:
		var req struct {
			Enabled  *bool                  `json:"enabled"`
			Port     *int                   `json:"port"`
			Strategy *model.RoutingStrategy `json:"strategy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		current := s.tunnel.Config()
		if req.Enabled != nil {
			current.Enabled = *req.Enabled
		}
		if req.Port != nil && *req.Port > 0 && *req.Port <= 65535 {
			current.Port = *req.Port
		}
		if req.Strategy != nil {
			current.Strategy = *req.Strategy
		}

		if err := s.tunnel.UpdateConfig(current); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update tunnel: %v", err))
			return
		}

		appCfg := s.store.GetConfig()
		appCfg.Tunnel = current
		s.store.UpdateConfig(appCfg)

		s.BroadcastSSE("tunnel_updated", current)
		writeJSON(w, http.StatusOK, current)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]string{"error": message})
}
