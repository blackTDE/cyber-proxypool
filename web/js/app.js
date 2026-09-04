const state = {
  status: {},
  config: {},
  subscriptions: [],
  nodes: [],
  tunnel: { enabled: true, port: 10808, strategy: 'round-robin', is_running: false },
  filter: {
    search: '',
    protocol: 'all',
    subId: 'all',
    runningOnly: false,
    sortBy: 'default'
  },
  isTestingAll: false
};

// --- Initialization ---
document.addEventListener('DOMContentLoaded', () => {
  initTheme();
  initSSE();
  fetchInitialData();
  setupEventListeners();

  // Polling fallback every 15s to keep counters fresh
  setInterval(() => {
    fetchStatus();
  }, 15000);
});

async function fetchInitialData() {
  await Promise.all([
    fetchStatus(),
    fetchConfig(),
    fetchSubscriptions(),
    fetchNodes(),
    fetchTunnel()
  ]);
}

// --- SSE Real-time Updates ---
function initSSE() {
  const eventSource = new EventSource('/api/events');

  eventSource.addEventListener('connected', () => {
    console.log('[SSE] Connected to event stream');
  });

  eventSource.addEventListener('config_updated', (e) => {
    state.config = JSON.parse(e.data);
    renderConfig();
    fetchStatus();
  });

  eventSource.addEventListener('node_started', (e) => {
    const data = JSON.parse(e.data);
    updateNodeState(data.id, { is_running: true, inbound_port: data.port });
    fetchStatus();
  });

  eventSource.addEventListener('node_stopped', (e) => {
    const data = JSON.parse(e.data);
    updateNodeState(data.id, { is_running: false, inbound_port: 0 });
    fetchStatus();
  });

  eventSource.addEventListener('node_tested', (e) => {
    const data = JSON.parse(e.data);
    updateNodeState(data.node_id, {
      latency: data.latency,
      exit_ip: data.exit_ip,
      last_tested_at: data.tested_at,
      error_message: data.error
    });
  });

  eventSource.addEventListener('nodes_updated', () => {
    fetchNodes();
    fetchStatus();
  });

  eventSource.addEventListener('test_complete', () => {
    state.isTestingAll = false;
    updateTestButton();
    showToast('Speed and Exit-IP test completed', 'emerald');
    fetchNodes();
  });

  eventSource.addEventListener('subscription_added', () => {
    fetchSubscriptions();
    fetchNodes();
    fetchStatus();
  });

  eventSource.addEventListener('subscription_refreshed', () => {
    fetchSubscriptions();
    fetchNodes();
    fetchStatus();
  });

  eventSource.addEventListener('subscription_deleted', () => {
    fetchSubscriptions();
    fetchNodes();
    fetchStatus();
  });

  eventSource.addEventListener('tunnel_updated', (e) => {
    fetchTunnel();
    fetchStatus();
  });

  eventSource.onerror = () => {
    console.warn('[SSE] Reconnecting...');
  };
}

function updateNodeState(nodeId, changes) {
  const node = state.nodes.find(n => n.id === nodeId);
  if (node) {
    Object.assign(node, changes);
    const row = document.getElementById(`node-row-${nodeId}`);
    if (row) {
      row.innerHTML = buildNodeRowHTML(node);
    } else {
      renderNodes();
    }
  }
}

// --- API Calls ---
async function fetchConfig() {
  try {
    const res = await fetch('/api/config');
    state.config = await res.json();
    renderConfig();
  } catch (err) {
    console.error('Failed to fetch config', err);
  }
}

async function fetchStatus() {
  try {
    const res = await fetch('/api/status');
    state.status = await res.json();
    renderStatus();
  } catch (err) {
    console.error('Failed to fetch status', err);
  }
}

async function fetchSubscriptions() {
  try {
    const res = await fetch('/api/subscriptions');
    state.subscriptions = await res.json();
    renderSubscriptionsSelect();
    renderSubscriptionsModalList();
  } catch (err) {
    console.error('Failed to fetch subscriptions', err);
  }
}

async function fetchNodes() {
  try {
    const res = await fetch('/api/nodes');
    state.nodes = await res.json();
    renderNodes();
  } catch (err) {
    console.error('Failed to fetch nodes', err);
  }
}

async function fetchTunnel() {
  try {
    const res = await fetch('/api/tunnel');
    state.tunnel = await res.json();
    renderTunnel();
  } catch (err) {
    console.error('Failed to fetch tunnel config', err);
  }
}

// --- UI Actions ---
async function handleListenAll() {
  try {
    showToast('Starting all proxy node listeners...', 'cyan');
    const res = await fetch('/api/nodes/start-all', { method: 'POST' });
    const data = await res.json();
    showToast(`Started ${data.started} node listeners!`, 'emerald');
    await fetchNodes();
    await fetchStatus();
  } catch (err) {
    showToast(`Failed to start all: ${err.message}`, 'danger');
  }
}

async function handleStopAll() {
  try {
    showToast('Stopping all listeners...', 'cyan');
    await fetch('/api/nodes/stop-all', { method: 'POST' });
    showToast('All node listeners stopped', 'cyan');
    await fetchNodes();
    await fetchStatus();
  } catch (err) {
    showToast(`Failed to stop all: ${err.message}`, 'danger');
  }
}

async function handleTestAll() {
  if (state.isTestingAll) return;
  state.isTestingAll = true;
  updateTestButton();
  showToast('Initiating concurrent speed & IP testing...', 'cyan');
  try {
    await fetch('/api/nodes/test-all', { method: 'POST' });
  } catch (err) {
    state.isTestingAll = false;
    updateTestButton();
    showToast(`Failed to initiate test: ${err.message}`, 'danger');
  }
}

function updateTestButton() {
  const btn = document.getElementById('btnTestAll');
  if (!btn) return;
  if (state.isTestingAll) {
    btn.innerHTML = `
      <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M21 12a9 9 0 1 1-6.219-8.56"/>
      </svg>
      Testing All...
    `;
    btn.disabled = true;
  } else {
    btn.innerHTML = `
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
      </svg>
      Test All Latencies & IPs
    `;
    btn.disabled = false;
  }
}

async function toggleNode(nodeId, start) {
  const endpoint = start ? `/api/nodes/${nodeId}/start` : `/api/nodes/${nodeId}/stop`;
  try {
    const res = await fetch(endpoint, { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed');
    }
    const data = await res.json();
    if (start) {
      showToast(`Node listening on port ${data.port}`, 'emerald');
    } else {
      showToast('Node stopped', 'cyan');
    }
    fetchNodes();
    fetchStatus();
  } catch (err) {
    showToast(`Action failed: ${err.message}`, 'danger');
    fetchNodes();
  }
}

async function testSingleNode(nodeId) {
  showToast('Testing node latency...', 'cyan');
  try {
    const res = await fetch(`/api/nodes/${nodeId}/test`, { method: 'POST' });
    const data = await res.json();
    if (data.success) {
      showToast(`Latency: ${data.latency}ms, IP: ${data.exit_ip || 'N/A'}`, 'emerald');
    } else {
      showToast(`Unreachable: ${data.error || 'Connection timed out'}`, 'danger');
    }
    fetchNodes();
  } catch (err) {
    showToast(`Test failed: ${err.message}`, 'danger');
  }
}

async function handleAddSubscription(e) {
  e.preventDefault();
  const name = document.getElementById('subNameInput').value.trim();
  const activeTab = document.querySelector('.tab-btn.active').dataset.tab;
  
  let url = '';
  let content = '';

  if (activeTab === 'url') {
    url = document.getElementById('subUrlInput').value.trim();
    if (!url) {
      showToast('Please paste a subscription URL', 'danger');
      return;
    }
  } else {
    content = document.getElementById('subContentInput').value.trim();
    if (!content) {
      showToast('Please paste YAML or node links', 'danger');
      return;
    }
  }

  showToast('Parsing and extracting nodes...', 'cyan');
  try {
    const res = await fetch('/api/subscriptions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url, content })
    });

    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Failed to add subscription');
    }

    const data = await res.json();
    showToast(`Extracted ${data.node_count} nodes successfully!`, 'emerald');
    closeModal('subModal');
    document.getElementById('subForm').reset();
    fetchSubscriptions();
    fetchNodes();
    fetchStatus();
  } catch (err) {
    showToast(err.message, 'danger');
  }
}

async function refreshSub(subId) {
  showToast('Refreshing subscription...', 'cyan');
  try {
    const res = await fetch(`/api/subscriptions/${subId}/refresh`, { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Refresh failed');
    }
    const data = await res.json();
    showToast(`Updated! ${data.node_count} nodes active`, 'emerald');
    fetchSubscriptions();
    fetchNodes();
    fetchStatus();
  } catch (err) {
    showToast(err.message, 'danger');
  }
}

async function deleteSub(subId) {
  if (!confirm('Delete this subscription and all its nodes?')) return;
  try {
    const res = await fetch(`/api/subscriptions/${subId}`, { method: 'DELETE' });
    if (!res.ok) throw new Error('Delete failed');
    showToast('Subscription deleted', 'cyan');
    fetchSubscriptions();
    fetchNodes();
    fetchStatus();
  } catch (err) {
    showToast(err.message, 'danger');
  }
}

async function updateTunnelConfig(changes) {
  try {
    const res = await fetch('/api/tunnel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(changes)
    });
    if (!res.ok) throw new Error('Failed to update tunnel');
    const updated = await res.json();
    state.tunnel = updated;
    renderTunnel();
    showToast('Tunnel pool settings updated', 'emerald');
  } catch (err) {
    showToast(err.message, 'danger');
  }
}

// --- Renders ---
function renderStatus() {
  const s = state.status;
  if (!s) return;

  const totalEl = document.getElementById('metricTotalNodes');
  const runningEl = document.getElementById('metricRunningNodes');
  const subsEl = document.getElementById('metricSubs');
  const memEl = document.getElementById('metricMem');
  const basePortEl = document.getElementById('metricBasePort');

  if (totalEl) totalEl.innerHTML = `${s.total_nodes || 0}`;
  if (runningEl) runningEl.innerHTML = `${s.running_nodes || 0} <small>/ ${s.total_nodes || 0} active</small>`;
  if (subsEl) subsEl.innerHTML = `${s.subscriptions || 0}`;
  if (memEl) memEl.innerHTML = `${(s.memory_alloc_mb || 0).toFixed(1)} <small>MB</small>`;

  const bp = s.base_inbound_port || (state.config && state.config.base_inbound_port) || 20001;
  if (basePortEl) {
    basePortEl.innerHTML = `${bp} <small id="metricBasePortSmall">range: ${bp}+</small>`;
  }
}

function renderConfig() {
  if (!state.config) return;
  const bp = state.config.base_inbound_port || 20001;
  const basePortInput = document.getElementById('settingBasePort');
  const testUrlInput = document.getElementById('settingTestURL');
  const timeoutInput = document.getElementById('settingTimeoutSec');
  const modalTunnelPort = document.getElementById('settingsModalTunnelPort');

  if (basePortInput) basePortInput.value = bp;
  if (testUrlInput) testUrlInput.value = state.config.test_url || 'https://cloudflare.com/cdn-cgi/trace';
  if (timeoutInput) timeoutInput.value = state.config.test_timeout_sec || 8;
  if (modalTunnelPort) modalTunnelPort.textContent = (state.config.tunnel && state.config.tunnel.port) || 10808;

  const basePortEl = document.getElementById('metricBasePort');
  if (basePortEl) {
    basePortEl.innerHTML = `${bp} <small id="metricBasePortSmall">range: ${bp}+</small>`;
  }
}

function openSettingsModal() {
  renderConfig();
  openModal('settingsModal');
}

function renderTunnel() {
  const t = state.tunnel;
  const radar = document.getElementById('tunnelRadar');
  const toggle = document.getElementById('tunnelToggle');
  const stratSelect = document.getElementById('tunnelStrategySelect');
  const portInput = document.getElementById('tunnelPortInput');
  const listenHttp = document.getElementById('tunnelEndpointHttp');
  const listenSocks = document.getElementById('tunnelEndpointSocks');

  if (radar) {
    radar.className = t.is_running ? 'tunnel-radar active' : 'tunnel-radar';
  }
  if (toggle) {
    toggle.checked = t.is_running;
  }
  if (stratSelect) {
    stratSelect.value = t.strategy || 'round-robin';
  }
  if (portInput) {
    portInput.value = t.port || 10808;
  }
  if (listenHttp) {
    listenHttp.textContent = `HTTP: 127.0.0.1:${t.port || 10808}`;
  }
  if (listenSocks) {
    listenSocks.textContent = `SOCKS5: 127.0.0.1:${t.port || 10808}`;
  }
}

function renderSubscriptionsSelect() {
  const sel = document.getElementById('filterSubSelect');
  if (!sel) return;

  const current = sel.value;
  sel.innerHTML = '<option value="all">All Subscriptions</option>';
  state.subscriptions.forEach(sub => {
    const opt = document.createElement('option');
    opt.value = sub.id;
    opt.textContent = `${sub.name} (${sub.node_count})`;
    sel.appendChild(opt);
  });
  sel.value = current;
}

function renderSubscriptionsModalList() {
  const list = document.getElementById('subModalList');
  if (!list) return;

  if (state.subscriptions.length === 0) {
    list.innerHTML = '<div style="color:var(--text-muted); font-size:0.85rem; padding:0.5rem 0;">No subscriptions added yet.</div>';
    return;
  }

  list.innerHTML = state.subscriptions.map(sub => `
    <div class="sub-item">
      <div class="sub-item-info">
        <strong>${escapeHTML(sub.name)}</strong>
        <span>Format: ${sub.format.toUpperCase()} | Nodes: ${sub.node_count} ${sub.url ? '| URL: ' + escapeHTML(sub.url.substring(0, 40)) + '...' : ''}</span>
      </div>
      <div style="display:flex; gap:0.4rem;">
        ${sub.url ? `<button class="cyber-btn cyber-btn-cyan cyber-btn-sm" onclick="refreshSub('${sub.id}')">🔄 Refresh</button>` : ''}
        <button class="cyber-btn cyber-btn-danger cyber-btn-sm" onclick="deleteSub('${sub.id}')">🗑️ Delete</button>
      </div>
    </div>
  `).join('');
}

function renderNodes() {
  const tbody = document.getElementById('nodeTableBody');
  const countDisplay = document.getElementById('filteredNodeCount');
  if (!tbody) return;

  // Filter & Sort
  let list = state.nodes.filter(n => {
    if (state.filter.subId !== 'all' && n.sub_id !== state.filter.subId) return false;
    if (state.filter.protocol !== 'all' && n.protocol !== state.filter.protocol) return false;
    if (state.filter.runningOnly && !n.is_running) return false;
    if (state.filter.search) {
      const q = state.filter.search.toLowerCase();
      const matchName = (n.name || '').toLowerCase().includes(q);
      const matchServer = (n.server || '').toLowerCase().includes(q);
      const matchCountry = (n.country || '').toLowerCase().includes(q);
      const matchIP = (n.exit_ip || '').toLowerCase().includes(q);
      if (!matchName && !matchServer && !matchCountry && !matchIP) return false;
    }
    return true;
  });

  // Sort with stable standards (Never dynamic jumping)
  if (state.filter.sortBy === 'latency') {
    list.sort((a, b) => {
      const latA = a.latency > 0 ? a.latency : 999999;
      const latB = b.latency > 0 ? b.latency : 999999;
      if (latA !== latB) return latA - latB;
      return (a.name || '').localeCompare(b.name || '');
    });
  } else if (state.filter.sortBy === 'name') {
    list.sort((a, b) => (a.name || '').localeCompare(b.name || '', undefined, { numeric: true, sensitivity: 'base' }));
  } else if (state.filter.sortBy === 'protocol') {
    list.sort((a, b) => {
      if (a.protocol !== b.protocol) return a.protocol.localeCompare(b.protocol);
      return (a.name || '').localeCompare(b.name || '', undefined, { numeric: true, sensitivity: 'base' });
    });
  } else if (state.filter.sortBy === 'port') {
    list.sort((a, b) => {
      const pA = a.inbound_port || 0;
      const pB = b.inbound_port || 0;
      if (pA !== pB) return pB - pA;
      return (a.name || '').localeCompare(b.name || '');
    });
  } else {
    // Standard default: Group by subscription, then natural alphanumeric node name
    list.sort((a, b) => {
      const subA = a.sub_name || '';
      const subB = b.sub_name || '';
      if (subA !== subB) return subA.localeCompare(subB);
      return (a.name || '').localeCompare(b.name || '', undefined, { numeric: true, sensitivity: 'base' });
    });
  }

  if (countDisplay) {
    countDisplay.textContent = `Showing ${list.length} of ${state.nodes.length} nodes`;
  }

  if (list.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="7" style="text-align: center; padding: 2.5rem; color: var(--text-muted);">
          No nodes match your search criteria. Paste a subscription URL above to load nodes!
        </td>
      </tr>
    `;
    return;
  }

  tbody.innerHTML = list.map(n => `<tr id="node-row-${n.id}">${buildNodeRowHTML(n)}</tr>`).join('');
}

function buildNodeRowHTML(n) {
  // Protocol Badge
  const protoBadge = `<span class="badge badge-${n.protocol}">${n.protocol.toUpperCase()}</span>`;

  // Latency
  let latencyBadge = `<span class="latency-none">---</span>`;
  if (n.latency > 0) {
    let cls = 'latency-fast';
    if (n.latency > 500) cls = 'latency-slow';
    else if (n.latency > 200) cls = 'latency-med';
    latencyBadge = `<span class="${cls}">${n.latency} ms</span>`;
  } else if (n.latency === -1) {
    latencyBadge = `<span class="latency-slow" title="${escapeHTML(n.error_message || 'Timeout')}">TIMEOUT</span>`;
  }

  // Inbound Port Box
  let portBox = `<span style="color:var(--text-dark); font-family:var(--font-mono); font-size:0.8rem;">OFFLINE</span>`;
  if (n.is_running && n.inbound_port > 0) {
    portBox = `
      <span class="copy-pill" onclick="copyToClipboard('127.0.0.1:${n.inbound_port}', 'Local Port')">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
        </svg>
        ${n.inbound_port}
      </span>
    `;
  }

  // Exit IP
  const exitIPBadge = n.exit_ip ? `
    <span style="font-family:var(--font-mono); color:var(--neon-cyan); font-size:0.8rem;">
      ${n.flag || ''} ${escapeHTML(n.exit_ip)}
    </span>
  ` : `<span style="color:var(--text-dark); font-size:0.8rem;">Not tested</span>`;

  return `
    <td style="width: 45px; text-align: center;">
      <label class="cyber-switch">
        <input type="checkbox" ${n.is_running ? 'checked' : ''} onchange="toggleNode('${n.id}', this.checked)">
        <span class="slider"></span>
      </label>
    </td>
    <td style="min-width: 180px;">
      <div style="font-weight: 600; display:flex; align-items:center; gap:0.4rem;">
        <span>${n.flag || '🌐'}</span>
        <span title="${escapeHTML(n.name)}">${escapeHTML(n.name)}</span>
      </div>
      <div style="font-size: 0.72rem; color: var(--text-muted); font-family: var(--font-mono);">
        ${escapeHTML(n.sub_name || '')}
      </div>
    </td>
    <td>${protoBadge}</td>
    <td style="font-family: var(--font-mono); font-size: 0.8rem; color: var(--text-muted);">
      ${escapeHTML(n.server)}:${n.port}
    </td>
    <td>${portBox}</td>
    <td>
      <div style="display: flex; align-items: center; gap: 0.5rem;">
        ${latencyBadge}
        <button class="cyber-btn cyber-btn-sm" style="padding: 0.15rem 0.4rem; font-size: 0.7rem;" onclick="testSingleNode('${n.id}')" title="Test Latency">
          ⚡
        </button>
      </div>
    </td>
    <td>${exitIPBadge}</td>
  `;
}

// --- Event Listeners & Modals ---
function setupEventListeners() {
  // Master buttons
  document.getElementById('btnListenAll')?.addEventListener('click', handleListenAll);
  document.getElementById('btnStopAll')?.addEventListener('click', handleStopAll);
  document.getElementById('btnTestAll')?.addEventListener('click', handleTestAll);

  // Settings Modal
  document.getElementById('btnOpenSettingsModal')?.addEventListener('click', openSettingsModal);

  const settingsForm = document.getElementById('settingsForm');
  if (settingsForm) {
    settingsForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const basePort = parseInt(document.getElementById('settingBasePort')?.value, 10);
      const testUrl = document.getElementById('settingTestURL')?.value;
      const timeoutSec = parseInt(document.getElementById('settingTimeoutSec')?.value, 10);

      if (isNaN(basePort) || basePort < 1024 || basePort > 65000) {
        showToast('Base port must be between 1024 and 65000', 'danger');
        return;
      }

      try {
        const res = await fetch('/api/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            base_inbound_port: basePort,
            test_url: testUrl,
            test_timeout_sec: timeoutSec
          })
        });
        const result = await res.json();
        if (result.error) throw new Error(result.error);
        state.config = result.config;
        renderConfig();
        fetchStatus();
        closeModal('settingsModal');
        showToast(`Base inbound port updated to ${basePort}`, 'emerald');
      } catch (err) {
        showToast(err.message, 'danger');
      }
    });
  }

  // Search and filters
  document.getElementById('searchInput')?.addEventListener('input', (e) => {
    state.filter.search = e.target.value;
    renderNodes();
  });

  document.getElementById('filterSubSelect')?.addEventListener('change', (e) => {
    state.filter.subId = e.target.value;
    renderNodes();
  });

  document.querySelectorAll('.filter-proto-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.filter-proto-btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      state.filter.protocol = btn.dataset.proto;
      renderNodes();
    });
  });

  document.getElementById('filterRunningOnly')?.addEventListener('change', (e) => {
    state.filter.runningOnly = e.target.checked;
    renderNodes();
  });

  document.getElementById('sortSelect')?.addEventListener('change', (e) => {
    state.filter.sortBy = e.target.value;
    renderNodes();
  });

  // Tunnel controls
  document.getElementById('tunnelToggle')?.addEventListener('change', (e) => {
    updateTunnelConfig({ enabled: e.target.checked });
  });

  document.getElementById('tunnelStrategySelect')?.addEventListener('change', (e) => {
    updateTunnelConfig({ strategy: e.target.value });
  });

  document.getElementById('tunnelPortInput')?.addEventListener('change', (e) => {
    const port = parseInt(e.target.value, 10);
    if (port > 0 && port <= 65535) {
      updateTunnelConfig({ port });
    }
  });

  // Subscriptions Modal
  document.getElementById('btnOpenSubModal')?.addEventListener('click', () => {
    openModal('subModal');
  });

  document.getElementById('subForm')?.addEventListener('submit', handleAddSubscription);

  // Tab switching in modal
  document.querySelectorAll('.modal-tabs .tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.modal-tabs .tab-btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      const tab = btn.dataset.tab;
      document.getElementById('tabContentUrl').style.display = tab === 'url' ? 'block' : 'none';
      document.getElementById('tabContentRaw').style.display = tab === 'raw' ? 'block' : 'none';
    });
  });
}

function openModal(id) {
  const modal = document.getElementById(id);
  if (modal) modal.classList.add('active');
}

function closeModal(id) {
  const modal = document.getElementById(id);
  if (modal) modal.classList.remove('active');
}

// --- Utilities ---
function showToast(message, type = 'cyan') {
  const container = document.getElementById('toastContainer');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = 'toast';
  if (type === 'emerald') toast.style.borderColor = 'var(--neon-emerald)';
  if (type === 'danger') toast.style.borderColor = 'var(--neon-pink)';

  toast.innerHTML = `
    <span>${message}</span>
  `;

  container.appendChild(toast);
  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateY(10px)';
    toast.style.transition = 'all 0.3s';
    setTimeout(() => toast.remove(), 300);
  }, 3500);
}

function copyToClipboard(text, label = 'Content') {
  navigator.clipboard.writeText(text).then(() => {
    showToast(`Copied ${label}: ${text}`, 'emerald');
  }).catch(() => {
    showToast(`Failed to copy to clipboard`, 'danger');
  });
}

function escapeHTML(str) {
  if (!str) return '';
  return str.replace(/[&<>'"]/g, 
    tag => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[tag] || tag)
  );
}

// --- Cyber Daylight & Dark Theme Controller ---
function initTheme() {
  const toggleBtn = document.getElementById('btnThemeToggle');
  const modeText = document.getElementById('themeModeText');
  const metaThemeColor = document.getElementById('metaThemeColor');

  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('cyber_theme', theme);

    if (theme === 'light') {
      if (modeText) modeText.textContent = 'CYBER DARK';
      if (metaThemeColor) metaThemeColor.setAttribute('content', '#f1f5f9');
      if (toggleBtn) toggleBtn.setAttribute('title', 'Switch to Cyber Dark theme');
    } else {
      if (modeText) modeText.textContent = 'DAYLIGHT';
      if (metaThemeColor) metaThemeColor.setAttribute('content', '#06090f');
      if (toggleBtn) toggleBtn.setAttribute('title', 'Switch to Daylight theme');
    }
  }

  // Initial sync from attribute or system
  const initialTheme = document.documentElement.getAttribute('data-theme') || 
    localStorage.getItem('cyber_theme') || 
    (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
  applyTheme(initialTheme);

  if (toggleBtn) {
    toggleBtn.addEventListener('click', () => {
      const activeTheme = document.documentElement.getAttribute('data-theme') || 'dark';
      const nextTheme = activeTheme === 'light' ? 'dark' : 'light';
      applyTheme(nextTheme);
      showToast(`Switched to ${nextTheme === 'light' ? 'Daylight (Solar Matrix)' : 'Cyber Dark'} Theme`, 'cyan');
    });
  }

  // Auto-switch if system scheme changes and user hasn't explicitly set preference
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', (e) => {
    if (!localStorage.getItem('cyber_theme')) {
      applyTheme(e.matches ? 'light' : 'dark');
    }
  });
}
