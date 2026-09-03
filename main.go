package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cyberproxypool/pkg/api"
	"cyberproxypool/pkg/proxy"
	"cyberproxypool/pkg/storage"
	"cyberproxypool/pkg/tester"
)

//go:embed web/*
var embeddedWebFS embed.FS

const banner = `
   ______      __             ____                          ____             __
  / ____/_  __/ /_  ___  ____/ / __ \_________  _  ____  __/ __ \____  ____  / /
 / /   / / / / __ \/ _ \/ __  / /_/ / ___/ __ \| |/_/ / / / /_/ / __ \/ __ \/ / 
/ /___/ /_/ / /_/ /  __/ /_/ / ____/ /  / /_/ />  </ /_/ / ____/ /_/ / /_/ / /  
\____/\__, /_.___/\___/\__,_/_/   /_/   \____/_/|_|\__, /_/    \____/\____/_/   
     /____/                                       /____/                         
                      -- Autonomous Single-Binary Engine --
`

func main() {
	var (
		flagHost       = flag.String("host", "0.0.0.0", "Web UI and Proxy bind host")
		flagPort       = flag.Int("port", 8088, "Web UI dashboard port")
		flagBasePort   = flag.Int("base-port", 20001, "Base port for individual node proxy inbounds")
		flagTunnelPort = flag.Int("tunnel-port", 10808, "Unified rotating tunnel proxy port")
		flagDataDir    = flag.String("data-dir", "./data", "Data persistence directory")
	)
	flag.Parse()

	fmt.Print(banner)

	// 1. Initialize persistent storage
	store, err := storage.NewStore(*flagDataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	cfg := store.GetConfig()
	if *flagPort != 8088 {
		cfg.WebPort = *flagPort
	}
	if *flagHost != "0.0.0.0" {
		cfg.WebHost = *flagHost
	}
	if *flagBasePort != 20001 {
		cfg.BaseInboundPort = *flagBasePort
	}
	if *flagTunnelPort != 10808 {
		cfg.Tunnel.Port = *flagTunnelPort
	}
	_ = store.UpdateConfig(cfg)

	// 2. Initialize Tunnel Pool
	tunnelPool := proxy.NewTunnelPool(cfg.Tunnel, cfg.WebHost)
	if cfg.Tunnel.Enabled {
		if err := tunnelPool.Start(); err != nil {
			log.Printf("[WARN] Failed to start tunnel pool on port %d: %v", cfg.Tunnel.Port, err)
		} else {
			log.Printf("[INFO] Unified Rotating Tunnel Pool listening on %s:%d (DUAL HTTP & SOCKS5)", cfg.WebHost, cfg.Tunnel.Port)
		}
	}

	// 3. Initialize Inbound Manager & Tester
	mgr := proxy.NewManager(cfg.WebHost, cfg.BaseInboundPort, tunnelPool)
	nodeTester := tester.NewTester(cfg.TestURL, cfg.TestTimeoutSec)

	// 4. Initialize API Server
	apiServer := api.NewServer(store, mgr, tunnelPool, nodeTester)

	mux := http.NewServeMux()
	apiServer.RegisterRoutes(mux)

	// 5. Serve Embedded Web Frontend
	webSubFS, err := fs.Sub(embeddedWebFS, "web")
	if err != nil {
		log.Fatalf("Failed to sub embedded web fs: %v", err)
	}
	fileServer := http.FileServer(http.FS(webSubFS))
	mux.Handle("/", fileServer)

	serverAddr := net.JoinHostPort(cfg.WebHost, fmt.Sprintf("%d", cfg.WebPort))
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	// Start HTTP server in background
	go func() {
		log.Printf("[INFO] CyberProxyPool Web Dashboard available at: http://127.0.0.1:%d", cfg.WebPort)
		log.Printf("[INFO] Inbound proxy port range starts at: %d", cfg.BaseInboundPort)
		log.Printf("[INFO] Ready to accept subscriptions and start proxy listeners.")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Graceful shutdown handling
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	<-stopChan
	log.Println("[INFO] Shutting down CyberProxyPool gracefully...")

	// Stop all active node listeners and tunnel pool
	_ = mgr.StopAll()
	_ = tunnelPool.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)

	log.Println("[INFO] CyberProxyPool stopped.")
}
