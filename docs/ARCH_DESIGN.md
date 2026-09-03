# CyberProxyPool: Architecture & Technical Design Specification

## 1. Overview
CyberProxyPool is a modern, high-performance, single-binary proxy pool service with a cyber-tech dark web dashboard. It enables users to paste subscription URLs (Clash YAML, Base64 node bundles, vmess/vless/trojan/ss URIs) or upload config files, automatically parses and tests the nodes, and provides:
1. **Independent Inbound Proxy Ports**: Each active node exposes a dedicated local HTTP/SOCKS5 proxy port (e.g. 20001, 20002...).
2. **Unified Aggregated Tunnel Port**: A single entry point (e.g. port 10808) that automatically load-balances / rotates outgoing exit nodes across requests (round-robin, random, best-latency).
3. **Embedded Native Engine**: Zero external binary dependencies (no need to manually download `v2ray-core` like legacy v2raypool).
4. **Single Binary Deployment**: Frontend web dashboard is embedded into the backend binary using Go's `embed` filesystem.

---

## 2. System Architecture

```
+---------------------------------------------------------------+
|                       Web Dashboard                           |
|          (Cyber Dark UI / Modern Glassmorphism)               |
+---------------------------------------------------------------+
                               | REST API / SSE
+---------------------------------------------------------------+
|                   CyberProxyPool Core (Go)                    |
|                                                               |
|  +--------------------+  +------------------+  +-----------+  |
|  | Subscription Mgr   |  | Node Repository  |  | Latency   |  |
|  | - Clash YAML parse |  | - Persistent DB  |  | Speed     |  |
|  | - Base64 decoder   |  | - Geo Tagging    |  | Tester    |  |
|  | - Auto-refresh     |  | - State tracker  |  | (HTTP/TLS)|  |
|  +--------------------+  +------------------+  +-----------+  |
|                                                               |
|  +---------------------------------------------------------+  |
|  |                  Inbound Proxy Manager                  |  |
|  |  - Node Listeners: Port 20001 -> Node A                 |  |
|  |                    Port 20002 -> Node B                 |  |
|  |                    ...                                  |  |
|  |  - Unified Tunnel Pool: Port 10808 (Round-Robin/Random) |  |
|  |  - Dual Protocol: HTTP CONNECT + SOCKS5 on same port    |  |
|  +---------------------------------------------------------+  |
|                                                               |
|  +---------------------------------------------------------+  |
|  |                 Outbound Dialers Engine                 |  |
|  |  - Trojan Dialer (TLS / TCP)                            |  |
|  |  - Shadowsocks Dialer (AEAD ciphers)                    |  |
|  |  - VMess Dialer (AEAD / WS / TLS)                       |  |
|  |  - VLESS Dialer (TCP / WS / TLS)                        |  |
|  |  - SOCKS5 / HTTP Outbound Dialers                       |  |
|  +---------------------------------------------------------+  |
+---------------------------------------------------------------+
```

---

## 3. Key Components

### 3.1 Subscription Parser (`pkg/parser`)
- **Clash YAML format**: parses `proxies` array supporting `trojan`, `ss`, `vmess`, `vless`, `socks5`, `http`.
- **Base64 subscriptions**: standard base64/base64url decoded into newline-separated URI scheme list.
- **URI parsing**:
  - `trojan://password@server:port?sni=...#name`
  - `ss://base64(method:password@server:port)#name` and SIP002 URIs
  - `vmess://base64(json)`
  - `vless://uuid@server:port?encryption=none&security=tls&type=ws&path=/...#name`
  - `socks5://...`, `http://...`
- **Auto Geo Tagging**: extracts country/region flags (🇭🇰 HK, 🇯🇵 JP, 🇺🇸 US, 🇸🇬 SG, 🇩🇪 DE, etc.) from node names.

### 3.2 Outbound Dialers Engine (`pkg/dialer`)
- Implements standard `net.Dialer` interface for each proxy protocol.
- Direct TCP/TLS/WebSocket handshake with remote proxy servers.
- Eliminates the need for external `v2ray-core` binary orchestration.

### 3.3 Inbound Proxy Listener (`pkg/proxy`)
- **Dual-protocol detection**: Inspects first byte of incoming connection.
  - Byte `0x05`: SOCKS5 handshake -> invokes SOCKS5 handler.
  - Characters `CONNECT`, `GET`, `POST`: HTTP Proxy handshake -> invokes HTTP Proxy handler.
  - This allows clients to configure either HTTP or SOCKS5 on the exact same port!
- **Tunnel Pool**: Maintains an active ring of alive nodes. Routes incoming client requests across available nodes with strategies:
  - `round-robin`: sequential distribution
  - `random`: random distribution
  - `least-latency`: chooses fastest tested node

### 3.4 Speed & Exit-IP Tester (`pkg/tester`)
- Connects through the node's outbound dialer.
- Sends HTTP GET request to test target (default `https://api.ipify.org` or `https://httpbin.org/ip` or `https://www.google.com/generate_204`).
- Captures:
  - Real round-trip latency (ms)
  - Real outgoing public IP address
  - Health/alive status

### 3.5 Storage & Config (`pkg/storage`)
- Stores subscriptions, nodes, settings, and pool configurations in local JSON file with thread-safe locking and atomic writes.

### 3.6 Web UI (`web/`)
- Modern cyber-tech aesthetic with neon cyan (`#00f0ff`), electric emerald (`#00ff9d`), dark titanium background (`#090d16`, `#0e131f`), glassmorphism cards, and cyber borders.
- Real-time controls:
  - "Start All" / "Stop All" listener buttons
  - Single-node listener toggle switch
  - One-click "Test All Latencies & Detect IPs"
  - Quick-copy proxy address (`127.0.0.1:20001`, etc.)
  - Subscription management modal (Paste URL, Paste raw YAML/links, auto-refresh interval)
  - Unified tunnel pool settings (Port, routing mode, active node count)
  - Live activity stats: total nodes, running ports, average latency, traffic bytes.
