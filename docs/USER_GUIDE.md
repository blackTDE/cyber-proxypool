# CyberProxyPool: Complete User & Integration Guide

## 1. Introduction
CyberProxyPool is designed to eliminate the complexity of running proxy pools for web scraping, privacy research, and anti-crawling circumvention. It provides:
1. One-click node ingestion from any Clash YAML subscription, Base64 bundle, or URI list.
2. A sleek dark cyber dashboard for visual monitoring and control.
3. Dual-protocol local inbounds (HTTP + SOCKS5) for individual nodes.
4. An automated rotating tunnel proxy port for effortless IP rotation.
5. Standalone single-binary deployment with zero external dependencies.

---

## 2. Subscription Management

### Importing via Remote Subscription URL
1. Open the web dashboard at `http://127.0.0.1:8088`.
2. Click **"Subscriptions"** in the top navigation bar.
3. Keep the **"Paste URL"** tab active.
4. (Optional) Provide a memorable label, e.g., `Overseas ISP Pool`.
5. Paste your subscription URL (e.g. from your proxy provider, Clash Verge, Clash for Windows, or v2ray subscription).
6. Click **"Import & Extract Nodes"**.
7. The system fetches the content, identifies whether it is Clash YAML or Base64, extracts all valid nodes, identifies their country flags, and populates the node matrix.

### Importing via Raw YAML or Links
If your subscription is blocked or provided as a local YAML file:
1. Click **"Subscriptions"** ➡️ switch to **"Paste YAML / Node Links"** tab.
2. Paste the full Clash YAML content or lines of `vmess://`, `vless://`, `trojan://`, `ss://` links.
3. Click **"Import & Extract Nodes"**.

### Managing Multiple Subscriptions
- You can add any number of subscriptions.
- Use the **"Subscriptions"** dropdown on the main toolbar to filter the matrix to a specific subscription or view all.
- Click **"🔄 Refresh"** next to any remote subscription to pull the latest nodes without affecting your custom configuration.

---

## 3. Proxy Listener Operations

### Starting and Stopping Node Inbounds
- **Listen All**: Click the emerald **"Listen All"** button in the header. The manager assigns each node a unique local port (starting from `20001`, `20002`, etc.) and starts the listener.
- **Stop All**: Click the red **"Stop All"** button to shut down all running listeners immediately.
- **Manual Toggle**: Use the switch toggle on the left of any node row to start or stop that specific node.

### Local Inbound Usage
Each active port accepts both HTTP and SOCKS5 traffic:
```bash
# SOCKS5
curl -x socks5h://127.0.0.1:20001 https://api.ipify.org

# HTTP
curl -x http://127.0.0.1:20001 https://api.ipify.org
```

---

## 4. Smart Rotating Tunnel Pool

The **Unified Rotating Tunnel Pool** (`127.0.0.1:10808` by default) routes every incoming request to one of the currently active nodes.

### Routing Strategies
- **Round-Robin** (Default): Requests cycle sequentially through available nodes.
- **Random**: Requests are assigned randomly across available nodes.
- **Lowest Latency**: Requests prefer the node with the fastest latency recorded during the latest speed test.

### Scraper Integration Examples

#### Python (Requests / Urllib3)
```python
import requests

proxy = "http://127.0.0.1:10808"
proxies = {
    "http": proxy,
    "https": proxy
}

for i in range(5):
    response = requests.get("https://api.ipify.org?format=json", proxies=proxies)
    print(f"Request {i+1} Exit IP:", response.json()["ip"])
```

#### Scrapy (`settings.py`)
```python
DOWNLOADER_MIDDLEWARES = {
    'scrapy.downloadermiddlewares.httpproxy.HttpProxyMiddleware': 750,
}

HTTP_PROXY = 'http://127.0.0.1:10808'
```

#### Node.js (Axios / HttpsProxyAgent)
```javascript
import axios from 'axios';
import { HttpsProxyAgent } from 'https-proxy-agent';

const agent = new HttpsProxyAgent('http://127.0.0.1:10808');

async function testRotation() {
  for (let i = 0; i < 5; i++) {
    const res = await axios.get('https://api.ipify.org?format=json', { httpsAgent: agent });
    console.log(`Request ${i + 1} Exit IP:`, res.data.ip);
  }
}
testRotation();
```

---

## 5. Latency & Exit-IP Verification
- Click **"Test All Latencies & IPs"** to start the concurrent speed and IP detection engine.
- As results arrive, the table updates in real time via Server-Sent Events (SSE).
- You can sort by **"Lowest Latency"** to easily see the best performing nodes.
