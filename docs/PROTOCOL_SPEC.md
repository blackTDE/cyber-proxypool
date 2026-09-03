# CyberProxyPool: Protocol & Subscription Specification

## 1. Supported Subscription Input Types
The application accepts multiple subscription formats seamlessly:
1. **Remote HTTP/HTTPS URL**:
   - Clash YAML Subscription (returns `proxies:` list)
   - Standard Base64 Subscription (returns base64-encoded lines of `vmess://`, `vless://`, `ss://`, `trojan://`)
   - Plaintext Node List (returns raw URLs separated by newline)
2. **Raw Text / File Content**:
   - Direct paste or upload of `.yaml` / `.yml` Clash config
   - Direct paste of base64 string or multiple URI links

---

## 2. Supported Outbound Protocols

| Protocol | Schemes | Transports | Security |
|----------|---------|------------|----------|
| **Trojan** | `trojan://` | TCP, WebSocket | TLS (SNI, Skip-Cert-Verify) |
| **Shadowsocks** | `ss://` | TCP | AEAD (aes-128-gcm, aes-256-gcm, chacha20-ietf-poly1305) |
| **VMess** | `vmess://` | TCP, WebSocket | TLS, Plain |
| **VLESS** | `vless://` | TCP, WebSocket | TLS, Plain |
| **SOCKS5** | `socks5://`, `socks://` | TCP | None / User-Pass |
| **HTTP** | `http://` | TCP | CONNECT tunnel |

---

## 3. Inbound Proxy Ports & Tunnel Mechanism

### 3.1 Node Specific Inbounds
- Start Port default: `20001`
- When Node 1 is started: Listens on `127.0.0.1:20001`
- When Node 2 is started: Listens on `127.0.0.1:20002`
- Protocol: Dual HTTP & SOCKS5 auto-detection on the same port!
  - `curl -x http://127.0.0.1:20001 https://api.ipify.org`
  - `curl -x socks5h://127.0.0.1:20001 https://api.ipify.org`

### 3.2 Unified Tunnel Pool Inbound
- Tunnel Port default: `10808`
- Routing Policies:
  - `round-robin`: Every incoming TCP connection picks the next active node in order.
  - `random`: Every incoming TCP connection randomly selects an active node.
  - `best-latency`: Prefers the node with lowest latency from latest speed test.
- Dynamic failover: If a node connection fails, it falls back to another active node.

---

## 4. REST API Endpoints

- `GET /api/status`: System overview (version, running listeners count, tunnel status, mem/goroutines).
- `GET /api/subscriptions`: List all subscriptions.
- `POST /api/subscriptions`: Add new subscription URL or raw config.
- `DELETE /api/subscriptions/:id`: Delete subscription and associated nodes.
- `POST /api/subscriptions/:id/refresh`: Force pull and re-extract nodes for subscription.
- `GET /api/nodes`: List parsed nodes with filtering, pagination, status, and ports.
- `POST /api/nodes/:id/start`: Start local proxy listener for node.
- `POST /api/nodes/:id/stop`: Stop local proxy listener for node.
- `POST /api/nodes/start-all`: Start proxy listeners for all (or filtered) nodes.
- `POST /api/nodes/stop-all`: Stop all active proxy listeners.
- `POST /api/nodes/:id/test`: Test latency and fetch exit IP for a specific node.
- `POST /api/nodes/test-all`: Concurrent latency and exit IP test for all nodes.
- `GET /api/tunnel`: Get tunnel pool settings and status.
- `POST /api/tunnel`: Update tunnel settings (enable/disable, port, routing strategy).
- `GET /api/events`: Server-Sent Events (SSE) stream for live updates (test progress, listener state).
