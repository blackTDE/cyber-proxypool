# CyberProxyPool ⚡

<div align="center">
  <h3>Next-Generation Autonomous Single-Binary Proxy Pool & Rotating Tunnel Engine</h3>
  <p>Modern • Cyber Dark Tech Dashboard • Zero External Dependencies • Instant Deploy</p>
</div>

---

## 🌟 Highlights & Comparison with Legacy `v2raypool`

| Feature | Legacy `v2raypool` | **CyberProxyPool** |
| :--- | :--- | :--- |
| **Deployment** | Requires manual zip download of external `v2ray-core`, `.env` config, multiple binaries | **Single standalone binary** with embedded Cyber Web UI (`//go:embed`). Zero external runtime dependencies! |
| **Web UI** | Traditional HTML table forms | **Modern Tech Cyber Dark UI** with glassmorphism, live neon telemetry, real-time SSE updates |
| **Subscription Parsing** | Limited (only Trojan in Clash) | **Multi-Format Auto-Detection**: Clash YAML, Base64 URI bundles, VMess, VLESS, Trojan, Shadowsocks, Socks5, HTTP |
| **Inbound Protocol** | Separate HTTP or Socks | **Dual-Protocol Inbound**: SOCKS5 and HTTP CONNECT auto-detected on the **exact same port** |
| **Unified Tunnel Pool** | Basic tunnel | **Smart Rotating Tunnel Pool** with Round-Robin, Random, and Lowest-Latency strategies |
| **Exit IP & Speed Test** | Basic latency | **Real Exit-IP Detection & Latency** verification with country flags & concurrency limiter |
| **Subscription Management** | Single file / URL | **Multi-Subscription Manager** with one-click refresh, custom labels, and raw YAML paste |

---

## 🚀 Quick Start

### 1. Build from Source
Ensure you have Go 1.22+ installed:
```bash
# Clone the repository
git clone https://github.com/ray/ip-proxy-pool.git
cd ip-proxy-pool

# Compile single binary with embedded web dashboard
go build -o bin/cyberproxypool -trimpath -ldflags="-s -w" .
```

### 2. Run the Service
```bash
# Run with default settings (Web UI on :8088, Tunnel on :10808, Node inbounds from :20001)
./bin/cyberproxypool

# Or customize ports:
./bin/cyberproxypool -port 8088 -tunnel-port 10808 -base-port 20001 -data-dir ./data
```

### 3. Access Web Dashboard
Open your browser at:
👉 **[http://127.0.0.1:8088](http://127.0.0.1:8088)**

---

## 🖥️ Web UI Dashboard Features

1. **One-Click Subscription Import**:
   - Click **"Subscriptions"** ➡️ Paste your subscription URL (Clash YAML or Base64 link).
   - Or paste raw Clash YAML text / node URI lines directly into the textarea.
   - Nodes are automatically parsed, geo-tagged with country flags (🇭🇰, 🇯🇵, 🇺🇸, 🇸🇬, 🇩🇪, etc.), and listed in the dashboard.

2. **One-Click Listener Control**:
   - **"Listen All"**: Starts local proxy listeners for every node concurrently.
   - **"Stop All"**: Stops all local proxy listeners instantly.
   - **Individual Toggle**: Toggle individual node listeners ON/OFF with smooth cyber switches.

3. **Unified Rotating Tunnel Pool (`127.0.0.1:10808`)**:
   - Acts as a single proxy entry point.
   - Every incoming request automatically rotates across active proxy nodes according to your selected policy:
     - **Round-Robin**: Sequentially cycles through nodes.
     - **Random**: Randomly selects an active node per connection.
     - **Lowest Latency**: Dynamically routes traffic through the lowest latency tested node.

4. **Latency & Exit-IP Verification**:
   - Click **"Test All Latencies & IPs"** to trigger concurrent health checks.
   - The dashboard displays real-time latency (ms) and the actual outgoing public IP address for each proxy.

---

## 💻 Programmatic Usage & Scrapers

### Using Individual Node Inbounds
Each running node listens on an assigned port (e.g. `20001`, `20002`):
```bash
# Using HTTP Proxy
curl -x http://127.0.0.1:20001 https://api.ipify.org

# Using SOCKS5 Proxy on the exact same port!
curl -x socks5h://127.0.0.1:20001 https://api.ipify.org
```

### Using the Unified Rotating Tunnel Pool
```bash
# Request 1 (Routes to Node A)
curl -x http://127.0.0.1:10808 https://api.ipify.org

# Request 2 (Routes to Node B - new exit IP!)
curl -x http://127.0.0.1:10808 https://api.ipify.org

# In Python:
import requests
proxies = {"http": "http://127.0.0.1:10808", "https": "http://127.0.0.1:10808"}
resp = requests.get("https://api.ipify.org?format=json", proxies=proxies)
print(resp.json())
```

---

## 📡 REST API Reference

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/status` | System health, memory, uptime, and active node counts |
| `GET` | `/api/subscriptions` | List all subscriptions |
| `POST` | `/api/subscriptions` | Add subscription via URL or raw content |
| `DELETE` | `/api/subscriptions/:id` | Delete subscription and associated nodes |
| `POST` | `/api/subscriptions/:id/refresh` | Refresh subscription from remote URL |
| `GET` | `/api/nodes` | List nodes (supports query params `sub_id`, `search`, `protocol`, `running_only`) |
| `POST` | `/api/nodes/start-all` | Start all proxy inbounds |
| `POST` | `/api/nodes/stop-all` | Stop all proxy inbounds |
| `POST` | `/api/nodes/test-all` | Concurrently test latency and exit IPs for all nodes |
| `POST` | `/api/nodes/:id/start` | Start individual node listener |
| `POST` | `/api/nodes/:id/stop` | Stop individual node listener |
| `POST` | `/api/nodes/:id/test` | Test single node latency and exit IP |
| `GET` | `/api/tunnel` | Get rotating tunnel configuration and status |
| `POST` | `/api/tunnel` | Update tunnel port, strategy, or state |
| `GET` | `/api/events` | Server-Sent Events (SSE) stream for real-time updates |

---

## 🛠️ Architecture & Tech Stack

- **Backend**: Go 1.22+ (High-performance non-blocking netpoll goroutines)
- **Protocols Supported**: Trojan (TCP/WS/TLS), Shadowsocks (AEAD), VMess, VLESS, SOCKS5, HTTP
- **Inbound Engine**: Custom dual-protocol detector for simultaneous HTTP CONNECT & SOCKS5
- **Frontend**: Pure modern cyber tech dashboard embedded with `embed.FS`
- **Persistence**: Atomic JSON storage engine with write-lock synchronization
