document.addEventListener('DOMContentLoaded', () => {
  const elements = {
    uptime: document.getElementById('stat-uptime'),
    requests: document.getElementById('stat-requests'),
    sessions: document.getElementById('stat-sessions'),
    defaultModelBadge: document.getElementById('stat-model'),
    inputTokens: document.getElementById('stat-input-tokens'),
    cacheBarFill: document.getElementById('cache-bar-fill'),
    cacheSub: document.getElementById('stat-cache-sub'),
    outputTokens: document.getElementById('stat-output-tokens'),
    telemetryTbody: document.getElementById('telemetry-tbody'),
    btnRefreshTelemetry: document.getElementById('btn-refresh-telemetry'),
    btnClearTelemetry: document.getElementById('btn-clear-telemetry'),
    
    // Tab 1: OpenCode
    modelSelect: document.getElementById('model-select'),
    saveModelBtn: document.getElementById('btn-save-model'),
    modelChips: document.getElementById('model-chips'),
    btnRefreshModels: document.getElementById('btn-refresh-models'),
    btnCopyModel: document.getElementById('btn-copy-model'),
    testModelSelect: document.getElementById('test-model-select'),
    btnCopyTestModel: document.getElementById('btn-copy-test-model'),
    testPrompt: document.getElementById('test-prompt'),
    btnTest: document.getElementById('btn-test'),
    testOutput: document.getElementById('test-output'),
    testStats: document.getElementById('test-stats'),
    apiKeyInput: document.getElementById('api-key-input'),
    configPath: document.getElementById('config-path'),
    promptTuningInput: document.getElementById('prompt-tuning-input'),
    proxyList: document.getElementById('proxy-list'),

    // Tab 1: WARP IPv6 Fallback Hero Card
    warpRoutePill: document.getElementById('warp-route-pill'),
    warpToggleEnabled: document.getElementById('warp-toggle-enabled'),
    warpDirectBadge: document.getElementById('warp-direct-badge'),
    warpDirectResetLine: document.getElementById('warp-direct-reset-line'),
    warpDirectCountdownLine: document.getElementById('warp-direct-countdown-line'),
    warpStateBadge: document.getElementById('warp-state-badge'),
    warpActiveIP: document.getElementById('warp-active-ip'),
    btnCopyWarpIP: document.getElementById('btn-copy-warp-ip'),
    warpMetaLine: document.getElementById('warp-meta-line'),
    warpIdleLine: document.getElementById('warp-idle-line'),
    btnWarpHop: document.getElementById('btn-warp-hop'),
    btnWarpStop: document.getElementById('btn-warp-stop'),
    btnWarpClearBurned: document.getElementById('btn-warp-clear-burned'),
    warpBurnedCountBtn: document.getElementById('warp-burned-count-btn'),
    warpBurnedCountSummary: document.getElementById('warp-burned-count-summary'),
    warpBurnedTbody: document.getElementById('warp-burned-tbody'),
    warpStat200: document.getElementById('warp-stat-200'),
    warpStat429: document.getElementById('warp-stat-429'),
    warpStat403: document.getElementById('warp-stat-403'),
    warpStatHops: document.getElementById('warp-stat-hops'),

    // Tab 2: Kilo Gateway Hub
    kiloModelSelect: document.getElementById('kilo-model-select'),
    btnRefreshKiloModels: document.getElementById('btn-refresh-kilo-models'),
    btnCopyKiloModel: document.getElementById('btn-copy-kilo-model'),
    kiloModelChips: document.getElementById('kilo-model-chips'),
    btnSaveKiloModel: document.getElementById('btn-save-kilo-model'),
    kiloTestModelSelect: document.getElementById('kilo-test-model-select'),
    btnCopyKiloTestModel: document.getElementById('btn-copy-kilo-test-model'),
    kiloTestPrompt: document.getElementById('kilo-test-prompt'),
    btnKiloTest: document.getElementById('btn-kilo-test'),
    kiloTestOutput: document.getElementById('kilo-test-output'),
    kiloTestStats: document.getElementById('kilo-test-stats'),
    
    // Tab 3: Puter AI (Browser Relay)
    relayStatusDot: document.getElementById('relay-status-dot'),
    relayStatusBadge: document.getElementById('relay-status-badge'),
    relayStatusDesc: document.getElementById('relay-status-desc'),
    relayAccountName: document.getElementById('relay-account-name'),
    relayStatsCount: document.getElementById('relay-stats-count'),
    btnPuterLogin: document.getElementById('btn-puter-login'),
    btnPuterSwitch: document.getElementById('btn-puter-switch'),
    btnPuterLogout: document.getElementById('btn-puter-logout'),
    btnSavePuterModel: document.getElementById('btn-save-puter-model'),
    puterModelSelect: document.getElementById('puter-model-select'),
    btnRefreshPuterModels: document.getElementById('btn-refresh-puter-models'),
    puterModelChips: document.getElementById('puter-model-chips'),
    btnCopyPuterModel: document.getElementById('btn-copy-puter-model'),
    puterTestModelSelect: document.getElementById('puter-test-model-select'),
    btnCopyPuterTestModel: document.getElementById('btn-copy-puter-test-model'),
    puterTestPrompt: document.getElementById('puter-test-prompt'),
    btnPuterTest: document.getElementById('btn-puter-test'),
    puterTestOutput: document.getElementById('puter-test-output'),
    puterTestStats: document.getElementById('puter-test-stats'),

    // Tab 4: Connect Guide
    quickModelId: document.getElementById('quick-model-id'),
    quickBaseUrl: document.getElementById('quick-base-url'),
    quickApiKey: document.getElementById('quick-api-key'),
    quickKiloModelId: document.getElementById('quick-kilo-model-id'),
    quickKiloBaseUrl: document.getElementById('quick-kilo-base-url'),
    quickPuterModelId: document.getElementById('quick-puter-model-id'),
    quickPuterBaseUrl: document.getElementById('quick-puter-base-url'),
    quickV2BaseUrl: document.getElementById('quick-v2-base-url'),
    btnCopyQuickModel: document.getElementById('btn-copy-quick-model'),
    btnCopyBaseUrl: document.getElementById('btn-copy-base-url'),
    btnCopyApiKey: document.getElementById('btn-copy-api-key'),
    btnCopyKiloBaseUrl: document.getElementById('btn-copy-kilo-base-url'),
    btnCopyQuickKiloModel: document.getElementById('btn-copy-quick-kilo-model'),
    btnCopyPuterBaseUrl: document.getElementById('btn-copy-puter-base-url'),
    btnCopyQuickPuterModel: document.getElementById('btn-copy-quick-puter-model'),
    btnCopyV2BaseUrl: document.getElementById('btn-copy-v2-base-url')
  };

  let currentConfig = null;
  let availableModels = [];
  let lastWarpStatus = null;
  let lastWarpFetchedAt = 0;

  // Tab switching
  document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const target = btn.getAttribute('data-tab');
      document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
      document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
      btn.classList.add('active');
      const pane = document.getElementById(target);
      if (pane) pane.classList.add('active');
    });
  });

  // Puter.js Browser Relay Auth Controls
  if (elements.btnPuterLogin) {
    elements.btnPuterLogin.onclick = async () => {
      if (!window.puter || !puter.auth) {
        alert('Puter.js SDK belum siap dimuat di browser.');
        return;
      }
      try {
        elements.btnPuterLogin.disabled = true;
        elements.btnPuterLogin.textContent = 'Membuka popup login...';
        await puter.auth.signIn();
        await notifyBridgeStatus();
      } catch (err) {
        console.warn('Puter login canceled or failed:', err);
      } finally {
        elements.btnPuterLogin.disabled = false;
        elements.btnPuterLogin.textContent = '🔑 Login ke Puter (Popup)';
      }
    };
  }

  if (elements.btnPuterSwitch) {
    elements.btnPuterSwitch.onclick = async () => {
      if (!window.puter || !puter.auth) return;
      try {
        elements.btnPuterSwitch.disabled = true;
        try { await puter.auth.signOut(); } catch (_) {}
        await puter.auth.signIn();
        await notifyBridgeStatus();
      } catch (err) {
        console.warn('Puter switch account failed:', err);
      } finally {
        elements.btnPuterSwitch.disabled = false;
      }
    };
  }

  if (elements.btnPuterLogout) {
    elements.btnPuterLogout.onclick = async () => {
      if (!window.puter || !puter.auth) return;
      if (!confirm('Yakin ingin logout dari akun Puter di browser ini?')) return;
      try {
        await puter.auth.signOut();
        await notifyBridgeStatus();
      } catch (err) {
        console.warn('Puter signOut failed:', err);
      }
    };
  }

  // Clipboard copy helper with visual animation
  async function copyToClipboard(text, btn) {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
    } catch (_) {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    if (btn) {
      const orig = btn.innerHTML;
      btn.classList.add('copied');
      btn.innerHTML = '✓ Copied!';
      setTimeout(() => {
        btn.classList.remove('copied');
        btn.innerHTML = orig;
      }, 1600);
    }
  }

  // Format seconds to human readable
  function formatUptime(seconds) {
    if (!seconds && seconds !== 0) return '-';
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m ${s}s`;
    if (m > 0) return `${m}m ${s}s`;
    return `${s}s`;
  }

  // Format token counts (e.g. 1.25M, 42.5k, 120)
  function formatTokens(n) {
    if (n === undefined || n === null) return '0';
    if (n >= 1000000) return (n / 1000000).toFixed(2) + 'M';
    if (n >= 1000) return (n / 1000).toFixed(1) + 'k';
    return Number(n).toLocaleString();
  }

  function formatTime(iso) {
    if (!iso) return '-';
    try {
      const d = new Date(iso);
      return d.toLocaleTimeString([], { hour12: false });
    } catch (_) {
      return '-';
    }
  }

  function formatLocalDateTime(iso) {
    if (!iso) return '-';
    try {
      const d = new Date(iso);
      if (isNaN(d.getTime())) return iso;
      return new Intl.DateTimeFormat('id-ID', {
        day: '2-digit',
        month: 'short',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
      }).format(d);
    } catch (_) {
      return iso;
    }
  }

  function formatCountdownID(seconds) {
    const s = Math.max(0, Math.floor(seconds || 0));
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    if (h > 0) return `${h}j ${m}m ${sec}d`;
    if (m > 0) return `${m}m ${sec}d`;
    return `${sec}d`;
  }

  function renderWarpStatus(data) {
    if (!data || !elements.warpRoutePill) return;
    lastWarpStatus = data;

    const elapsedSec = lastWarpFetchedAt > 0 ? Math.floor((Date.now() - lastWarpFetchedAt) / 1000) : 0;
    const enabled = Boolean(data.enabled);
    const direct = data.direct || {};
    const warp = data.warp || {};
    const stats = data.stats || {};
    const burned = Array.isArray(data.burned_active) ? data.burned_active : [];
    const state = String(warp.state || (enabled ? 'DORMANT' : 'DISABLED')).toUpperCase();

    if (elements.warpToggleEnabled && document.activeElement !== elements.warpToggleEnabled) {
      elements.warpToggleEnabled.checked = enabled;
    }

    // 1. Active Route Pill
    let pillClass = 'route-pill route-pill-direct';
    let pillText = '🟢 ACTIVE: DIRECT (ISP)';
    if (state === 'HOPPING' || state === 'STARTING') {
      pillClass = 'route-pill route-pill-hopping';
      pillText = '🟡 HOPPING IPv6...';
    } else if (state === 'EXHAUSTED') {
      pillClass = 'route-pill route-pill-exhausted';
      pillText = '🔴 WARP EXHAUSTED';
    } else if (data.active_route === 'warp_ipv6' || state === 'ACTIVE') {
      pillClass = 'route-pill route-pill-warp';
      pillText = '🔵 ACTIVE: WARP IPv6 FALLBACK';
    }
    elements.warpRoutePill.className = pillClass;
    elements.warpRoutePill.textContent = pillText;

    // 2. Primary Direct Panel (exact reset time + live countdown)
    const directHealthy = direct.healthy !== false;
    let directRem = Math.max(0, (direct.reset_in_seconds || 0) - elapsedSec);
    if (direct.reset_at) {
      const targetMs = Date.parse(direct.reset_at);
      if (!isNaN(targetMs)) {
        directRem = Math.max(0, Math.floor((targetMs - Date.now()) / 1000));
      }
    }
    if (directHealthy || directRem <= 0) {
      if (elements.warpDirectBadge) {
        elements.warpDirectBadge.textContent = '✅ Healthy';
        elements.warpDirectBadge.style.color = 'var(--accent-green)';
      }
      if (elements.warpDirectResetLine) {
        elements.warpDirectResetLine.textContent = 'Jalur utama siap dipakai (tidak ada cooldown aktif).';
      }
      if (elements.warpDirectCountdownLine) {
        const autoRet = currentConfig ? (currentConfig.warp_auto_return_direct !== false) : true;
        elements.warpDirectCountdownLine.textContent = `Auto-kembali ke Direct saat reset: ${autoRet ? 'ON' : 'OFF'}`;
      }
    } else {
      if (elements.warpDirectBadge) {
        elements.warpDirectBadge.textContent = '⏳ Cooldown';
        elements.warpDirectBadge.style.color = '#fbbf24';
      }
      if (elements.warpDirectResetLine) {
        elements.warpDirectResetLine.innerHTML = `Reset pada: <strong style="color:#fbbf24;">${escapeHtml(formatLocalDateTime(direct.reset_at))}</strong>`;
      }
      if (elements.warpDirectCountdownLine) {
        const src = direct.reset_source ? ` • sumber: ${direct.reset_source}` : '';
        const errInfo = direct.last_error ? ` (${direct.last_error})` : '';
        elements.warpDirectCountdownLine.textContent = `Tersisa: ${formatCountdownID(directRem)}${src}${errInfo}`;
      }
    }

    // 3. WARP IPv6 Fallback Panel (state, active IPv6, /64, colo, endpoint, idle timer)
    if (elements.warpStateBadge) {
      elements.warpStateBadge.textContent = state;
      if (state === 'ACTIVE') elements.warpStateBadge.style.color = 'var(--accent-cyan)';
      else if (state === 'HOPPING' || state === 'STARTING') elements.warpStateBadge.style.color = '#fbbf24';
      else if (state === 'EXHAUSTED') elements.warpStateBadge.style.color = 'var(--danger)';
      else if (state === 'DORMANT') elements.warpStateBadge.style.color = 'var(--accent-green)';
      else elements.warpStateBadge.style.color = 'var(--text-muted)';
    }

    const activeIP = warp.active_ip || '';
    if (elements.warpActiveIP) {
      if (activeIP) {
        elements.warpActiveIP.textContent = activeIP;
        elements.warpActiveIP.style.color = 'var(--accent-cyan)';
      } else if (state === 'DISABLED') {
        elements.warpActiveIP.textContent = '— (Nonaktif • 0 MB RAM)';
        elements.warpActiveIP.style.color = 'var(--text-muted)';
      } else {
        elements.warpActiveIP.textContent = '— (Dormant Standby • 0 MB RAM)';
        elements.warpActiveIP.style.color = 'var(--text-muted)';
      }
    }
    if (elements.btnCopyWarpIP) {
      elements.btnCopyWarpIP.style.display = activeIP ? 'inline-flex' : 'none';
    }

    if (elements.warpMetaLine) {
      if (activeIP) {
        const p64 = warp.active_prefix64 || '-';
        const colo = warp.colo || '-';
        const ep = warp.endpoint || '-';
        const hs = warp.handshake_ms ? `${(warp.handshake_ms / 1000).toFixed(2)}s` : '-';
        elements.warpMetaLine.textContent = `Prefix: ${p64} • Edge: ${colo} • EP: ${ep} • HS: ${hs}`;
      } else if (warp.last_error) {
        elements.warpMetaLine.textContent = `Last notice: ${warp.last_error}`;
      } else {
        const binTxt = warp.wireproxy_ready ? 'wireproxy ready' : 'auto-download on demand';
        const devTxt = warp.device_registered ? 'device cached' : 'auto-register 1x';
        elements.warpMetaLine.textContent = `Mode Lazy IPv6 • ${binTxt} • ${devTxt}`;
      }
    }

    if (elements.warpIdleLine) {
      const idleRem = Math.max(0, (warp.idle_shutdown_in_seconds || 0) - elapsedSec);
      if (state === 'ACTIVE' && idleRem > 0) {
        elements.warpIdleLine.style.display = 'block';
        elements.warpIdleLine.textContent = `Auto-stop idle dalam: ${formatCountdownID(idleRem)} (port socks5 :${warp.local_socks_port || '-'})`;
      } else {
        elements.warpIdleLine.style.display = 'none';
      }
    }

    // 4. Stats & Active Burned /64 Table
    if (elements.warpStat200) elements.warpStat200.textContent = String(stats.total_ok_200 || 0);
    if (elements.warpStat429) elements.warpStat429.textContent = String(stats.total_burned_429 || 0);
    if (elements.warpStat403) elements.warpStat403.textContent = String(stats.total_forbidden_403 || 0);
    if (elements.warpStatHops) elements.warpStatHops.textContent = String(stats.total_hops || 0);
    if (elements.warpBurnedCountBtn) elements.warpBurnedCountBtn.textContent = String(burned.length);
    if (elements.warpBurnedCountSummary) elements.warpBurnedCountSummary.textContent = String(burned.length);

    if (elements.warpBurnedTbody) {
      if (burned.length === 0) {
        elements.warpBurnedTbody.innerHTML = '<tr><td colspan="5" style="color: var(--text-muted); text-align: center; padding: 10px;">Belum ada prefix IPv6 yang sedang cooldown.</td></tr>';
      } else {
        elements.warpBurnedTbody.innerHTML = burned.map(b => {
          let rem = Math.max(0, (b.expires_in_seconds || 0) - elapsedSec);
          if (b.expires_at) {
            const ms = Date.parse(b.expires_at);
            if (!isNaN(ms)) rem = Math.max(0, Math.floor((ms - Date.now()) / 1000));
          }
          const stColor = b.http_status === 403 ? 'var(--danger)' : '#fbbf24';
          return `
            <tr>
              <td style="color: var(--accent-cyan);">${escapeHtml(b.prefix_64 || b.ip)}</td>
              <td>${escapeHtml(b.ip || '-')}</td>
              <td style="color: ${stColor};">HTTP ${b.http_status || 429} (${escapeHtml(b.reason || 'limit')})</td>
              <td>${escapeHtml(formatLocalDateTime(b.expires_at))}</td>
              <td style="font-weight: 600;">${formatCountdownID(rem)}</td>
            </tr>
          `;
        }).join('');
      }
    }
  }

  async function loadWarpStatus() {
    try {
      const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
      const res = await fetch('/api/warp/status', {
        headers: { 'Authorization': `Bearer ${apiKey}` }
      });
      if (!res.ok) return;
      const data = await res.json();
      lastWarpFetchedAt = Date.now();
      renderWarpStatus(data);
    } catch (e) {
      console.warn('Failed to load WARP status', e);
    }
  }

  function escapeHtml(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  // Load /health
  async function loadHealth() {
    try {
      const res = await fetch('/health');
      if (!res.ok) return;
      const data = await res.json();
      if (elements.uptime) elements.uptime.textContent = formatUptime(data.uptime_seconds);
      if (elements.requests) elements.requests.textContent = data.requests;
      if (elements.sessions) elements.sessions.textContent = data.sessions;
      if (elements.defaultModelBadge) elements.defaultModelBadge.textContent = data.default_model;

      if (elements.proxyList && data.proxies) {
        elements.proxyList.innerHTML = '';
        data.proxies.forEach(p => {
          const item = document.createElement('div');
          item.style.cssText = 'display: flex; justify-content: space-between; align-items: center; gap: 8px; background: var(--code-bg); padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border); font-family: var(--font-mono); font-size: 11px;';
          const addr = p.address || p.url || 'direct';
          const isUp = (p.healthy !== undefined) ? Boolean(p.healthy) : (p.status === 'active');
          const statusColor = isUp ? 'var(--accent-green)' : '#fbbf24';
          const label = addr === 'direct' ? '🌐 Direct (Local Machine Egress)' : `🛡️ Proxy: ${addr}`;
          const rem = p.cooldown_seconds_left || p.cooldown_remaining_sec || 0;
          const statusText = isUp
            ? 'ACTIVE'
            : `COOLDOWN (${formatCountdownID(rem)}${p.cooldown_until ? ` • reset ${formatLocalDateTime(p.cooldown_until)}` : ''})`;
          item.innerHTML = `
            <span style="color: ${isUp ? '#fff' : 'var(--text-muted)'};">${escapeHtml(label)}</span>
            <span style="color: ${statusColor}; font-weight: 600; text-align: right;">${escapeHtml(statusText)}</span>
          `;
          elements.proxyList.appendChild(item);
        });
      }

      if (data.warp) {
        lastWarpFetchedAt = Date.now();
        renderWarpStatus(data.warp);
      }
    } catch (e) {
      console.warn('Failed to load health', e);
    }
  }

  // Load /api/telemetry
  async function loadTelemetry() {
    try {
      const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
      const res = await fetch('/api/telemetry', {
        headers: { 'Authorization': `Bearer ${apiKey}` }
      });
      if (!res.ok) return;
      const data = await res.json();

      if (elements.inputTokens) {
        elements.inputTokens.textContent = formatTokens(data.total_prompt_tokens);
      }
      if (elements.outputTokens) {
        elements.outputTokens.textContent = formatTokens(data.total_output_tokens);
      }

      const hitPct = data.overall_cache_hit_pct !== undefined ? data.overall_cache_hit_pct.toFixed(1) : 0;
      if (elements.cacheBarFill) {
        elements.cacheBarFill.style.width = `${Math.min(100, Math.max(0, hitPct))}%`;
      }
      if (elements.cacheSub) {
        elements.cacheSub.textContent = `${hitPct}% Cache Hit (${formatTokens(data.total_cached_tokens)} cached)`;
      }

      if (elements.telemetryTbody) {
        const records = data.recent_records || [];
        if (records.length === 0) {
          elements.telemetryTbody.innerHTML = `<tr><td colspan="8" style="text-align: center; color: var(--text-muted); padding: 24px;">No requests recorded yet. Start making requests via Cline or Test Playground!</td></tr>`;
        } else {
          elements.telemetryTbody.innerHTML = '';
          records.forEach(r => {
            const row = document.createElement('tr');
            let pillClass = 'pill-hit-low';
            if (r.cache_hit_rate >= 70) pillClass = 'pill-hit-high';
            else if (r.cache_hit_rate >= 30) pillClass = 'pill-hit-med';

            const modelHtml = (r.requested_model && r.actual_model && r.requested_model !== r.actual_model)
              ? `<span class="model-tag"><span class="model-req">${escapeHtml(r.requested_model)}</span><span class="model-arrow">→</span><span class="model-actual">${escapeHtml(r.actual_model)}</span></span>`
              : `<span class="model-actual">${escapeHtml(r.actual_model || r.requested_model || '-')}</span>`;

            const statusBadge = (r.status_code === 200)
              ? '<span style="color: var(--accent-green); font-weight: 600;">200 OK</span>'
              : `<span style="color: var(--danger); font-weight: 600;">${r.status_code}</span>`;

            const isPuter = (r.lane === 'puter');
            const isKilo = (r.lane === 'kilo');
            let laneTagClass = 'lane-tag';
            if (isPuter) laneTagClass = 'lane-tag lane-puter';
            else if (isKilo) laneTagClass = 'lane-tag lane-kilo';

            let promptCell = `<span style="font-weight: 600;">${(r.prompt_tokens || 0).toLocaleString()}</span>`;
            let cacheCell = `<span class="pill-hit ${pillClass}">${r.cache_hit_rate || 0}% (${formatTokens(r.cached_tokens || 0)})</span>`;
            let outputCell = `<span>${(r.output_tokens || 0).toLocaleString()}</span>`;

            if (isPuter) {
              promptCell = `<span class="badge-token-hidden" title="Token metrics hidden for Puter direct requests">Direct</span>`;
              cacheCell = `<span style="color: var(--text-muted);">-</span>`;
              outputCell = `<span style="color: var(--text-muted);">-</span>`;
            }

            row.innerHTML = `
              <td style="color: var(--text-muted);">${formatTime(r.timestamp)}</td>
              <td>${modelHtml}</td>
              <td><span class="${laneTagClass}">${escapeHtml(r.lane || 'chat')}</span></td>
              <td>${promptCell}</td>
              <td>${cacheCell}</td>
              <td>${outputCell}</td>
              <td style="color: var(--text-muted);">${r.duration_ms || 0} ms</td>
              <td>${statusBadge}</td>
            `;
            elements.telemetryTbody.appendChild(row);
          });
        }
      }
    } catch (e) {
      console.warn('Failed to load telemetry', e);
    }
  }

  // Kilo verified free tier models list
  const kiloFeaturedModels = [
    'openrouter/free',
    'kilo-auto/free',
    'nvidia/nemotron-3-ultra-550b-a55b:free',
    'nvidia/nemotron-3.5-super-vl:free',
    'stepfun/step-3.7-flash:free',
    'stealth/glyph-cluster',
    'poolside/laguna-xs-2.1:free'
  ];

  let availableKiloModels = [];

  // Load Kilo models dynamically from /v2/kilo/models
  async function loadKiloModels(force = false) {
    try {
      const url = force ? '/v2/kilo/models?refresh=1' : '/v2/kilo/models';
      const res = await fetch(url);
      if (res.ok) {
        const json = await res.json();
        const list = (json.data || []).map(m => m.id);
        if (list.length > 0) {
          availableKiloModels = list;
          renderKiloModelOptions();
        }
      }
    } catch (e) {
      console.warn('Failed to load kilo models', e);
    }
  }

  function renderKiloModelOptions() {
    if (!elements.kiloModelSelect || !elements.kiloTestModelSelect) return;
    const prevVal = elements.kiloModelSelect.value;
    const prevTestVal = elements.kiloTestModelSelect.value;

    const models = availableKiloModels.length > 0 ? availableKiloModels : kiloFeaturedModels;

    const populate = (selectEl) => {
      selectEl.innerHTML = '';
      models.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m;
        opt.textContent = m;
        selectEl.appendChild(opt);
      });
    };

    populate(elements.kiloModelSelect);
    populate(elements.kiloTestModelSelect);

    const targetVal = prevVal && models.includes(prevVal) ? prevVal : (currentConfig?.kilo_default_model || 'openrouter/free');
    elements.kiloModelSelect.value = targetVal;
    elements.kiloTestModelSelect.value = prevTestVal && models.includes(prevTestVal) ? prevTestVal : targetVal;

    if (elements.quickKiloModelId) {
      elements.quickKiloModelId.textContent = targetVal;
    }

    renderKiloChips(targetVal);
  }

  function renderKiloChips(activeModel) {
    if (!elements.kiloModelChips) return;
    elements.kiloModelChips.innerHTML = '';
    const models = availableKiloModels.length > 0 ? availableKiloModels : kiloFeaturedModels;
    models.slice(0, 10).forEach(m => {
      const chip = document.createElement('span');
      chip.className = 'chip' + (m === activeModel ? ' active' : '');
      chip.textContent = m;
      chip.title = `Pilih model: ${m}`;
      chip.onclick = () => {
        if (elements.kiloModelSelect) elements.kiloModelSelect.value = m;
        if (elements.kiloTestModelSelect) elements.kiloTestModelSelect.value = m;
        if (elements.quickKiloModelId) elements.quickKiloModelId.textContent = m;
        document.querySelectorAll('#kilo-model-chips .chip').forEach(c => c.classList.remove('active'));
        chip.classList.add('active');
      };
      elements.kiloModelChips.appendChild(chip);
    });
  }

  // Presets Prompt Cepat untuk Kilo
  const KILO_PRESETS = {
    nalar: "Ada 3 kotak: satu apel saja, satu jeruk saja, dan satu apel+jeruk. Semua label salah. Anda boleh mengambil 1 buah dari 1 kotak dengan mata tertutup. Bagaimana cara menentukan isi semua kotak dengan pasti? Jelaskan langkah logisnya.",
    coding: "Tuliskan implementasi HTTP rate limiter berbasis token bucket thread-safe dalam bahasa Go lengkap dengan mutex dan unit test sederhana.",
    identitas: "Halo, siapa nama arsitektur modelmu, dari provider mana, dan berapa parameter atau context length yang kamu miliki? Jawab jujur dan ringkas."
  };

  document.querySelectorAll('.preset-kilo-btn').forEach(btn => {
    btn.onclick = () => {
      const key = btn.getAttribute('data-preset');
      if (KILO_PRESETS[key] && elements.kiloTestPrompt) {
        elements.kiloTestPrompt.value = KILO_PRESETS[key];
      }
    };
  });

  // Connect Refresh Kilo models button
  if (elements.btnRefreshKiloModels) {
    elements.btnRefreshKiloModels.onclick = async () => {
      elements.btnRefreshKiloModels.disabled = true;
      const orig = elements.btnRefreshKiloModels.textContent;
      elements.btnRefreshKiloModels.textContent = '🔄 Loading...';
      try {
        await loadKiloModels(true);
      } finally {
        elements.btnRefreshKiloModels.disabled = false;
        elements.btnRefreshKiloModels.textContent = orig;
      }
    };
  }

  // Puter verified working models list
  const puterFeaturedModels = [
    'claude-opus-5-5',
    'claude-sonnet-5-5',
    'claude-haiku-5-5',
    'deepseek-v4.1-flash-1017',
    'deepseek-v4.1-flash',
    'deepseek-v4-pro',
    'gpt-5.4',
    'gpt-4o',
    'gemini-3.8-flash'
  ];

  let availablePuterModels = [];

  // Load Puter models dynamically from /v2/puter/models
  async function loadPuterModels(force = false) {
    try {
      const url = force ? '/v2/puter/models?refresh=1' : '/v2/puter/models';
      const res = await fetch(url);
      if (res.ok) {
        const json = await res.json();
        const list = (json.data || []).map(m => m.id);
        if (list.length > 0) {
          availablePuterModels = list;
          renderPuterModelOptions();
        }
      }
    } catch (e) {
      console.warn('Failed to load puter models', e);
    }
  }

  function renderPuterModelOptions() {
    if (!elements.puterModelSelect || !elements.puterTestModelSelect) return;
    const prevVal = elements.puterModelSelect.value;
    const prevTestVal = elements.puterTestModelSelect.value;

    const models = availablePuterModels.length > 0 ? availablePuterModels : puterFeaturedModels;
    
    // Group models
    const claudeList = models.filter(m => m.toLowerCase().includes('claude'));
    const gptList = models.filter(m => m.toLowerCase().includes('gpt') || m.toLowerCase().startsWith('o1') || m.toLowerCase().startsWith('o3'));
    const deepseekList = models.filter(m => m.toLowerCase().includes('deepseek'));
    const otherList = models.filter(m => !claudeList.includes(m) && !gptList.includes(m) && !deepseekList.includes(m));

    const populate = (selectEl) => {
      selectEl.innerHTML = '';
      if (claudeList.length > 0) {
        const g = document.createElement('optgroup');
        g.label = `🌟 Anthropic Claude (${claudeList.length})`;
        claudeList.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          g.appendChild(opt);
        });
        selectEl.appendChild(g);
      }
      if (gptList.length > 0) {
        const g = document.createElement('optgroup');
        g.label = `🤖 OpenAI Models (${gptList.length})`;
        gptList.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          g.appendChild(opt);
        });
        selectEl.appendChild(g);
      }
      if (deepseekList.length > 0) {
        const g = document.createElement('optgroup');
        g.label = `⚡ DeepSeek Models (${deepseekList.length})`;
        deepseekList.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          g.appendChild(opt);
        });
        selectEl.appendChild(g);
      }
      if (otherList.length > 0) {
        const g = document.createElement('optgroup');
        g.label = `🌐 Other Flagship Models (${otherList.length})`;
        otherList.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          g.appendChild(opt);
        });
        selectEl.appendChild(g);
      }
    };

    populate(elements.puterModelSelect);
    populate(elements.puterTestModelSelect);

    const targetVal = prevVal && models.includes(prevVal) ? prevVal : (currentConfig?.puter_default_model || 'claude-opus-5-5');
    elements.puterModelSelect.value = targetVal;
    elements.puterTestModelSelect.value = prevTestVal && models.includes(prevTestVal) ? prevTestVal : targetVal;
    if (elements.quickPuterModelId) elements.quickPuterModelId.textContent = targetVal;

    renderPuterChips(targetVal);
  }

  function renderPuterChips(activeModel) {
    if (!elements.puterModelChips) return;
    elements.puterModelChips.innerHTML = '';
    const chips = puterFeaturedModels;
    chips.forEach(m => {
      const chip = document.createElement('span');
      chip.className = 'chip' + (m === activeModel ? ' active' : '');
      chip.textContent = m;
      chip.onclick = () => {
        if (elements.puterModelSelect) elements.puterModelSelect.value = m;
        if (elements.puterTestModelSelect) elements.puterTestModelSelect.value = m;
        if (elements.quickPuterModelId) elements.quickPuterModelId.textContent = m;
        document.querySelectorAll('#puter-model-chips .chip').forEach(c => c.classList.remove('active'));
        chip.classList.add('active');
      };
      elements.puterModelChips.appendChild(chip);
    });
  }

  // Presets Prompt Cepat untuk Puter
  const PUTER_PRESETS = {
    identitas: "Siapa namamu, siapa pembuatmu, dan versi model apa kamu sebenarnya? Jawab jujur dan detail.",
    cerita: "Tuliskan satu paragraf narasi dongeng anak yang hangat tentang seekor tupai kecil pembuat jam di hutan tua.",
    coding: "Tuliskan fungsi Python sederhana untuk mencari substring terpanjang tanpa karakter berulang, jelaskan langkahnya."
  };

  document.querySelectorAll('.preset-puter-btn').forEach(btn => {
    btn.onclick = () => {
      const key = btn.getAttribute('data-preset');
      if (PUTER_PRESETS[key] && elements.puterTestPrompt) {
        elements.puterTestPrompt.value = PUTER_PRESETS[key];
      }
    };
  });

  // Connect Refresh Puter models button
  if (elements.btnRefreshPuterModels) {
    elements.btnRefreshPuterModels.onclick = async () => {
      elements.btnRefreshPuterModels.disabled = true;
      const orig = elements.btnRefreshPuterModels.textContent;
      elements.btnRefreshPuterModels.textContent = '🔄 Loading...';
      try {
        await loadPuterModels(true);
      } finally {
        elements.btnRefreshPuterModels.disabled = false;
        elements.btnRefreshPuterModels.textContent = orig;
      }
    };
  }

  // Load /api/config
  async function loadConfig() {
    try {
      const res = await fetch('/api/config');
      if (res.ok) {
        const data = await res.json();
        currentConfig = data.config;
        if (elements.configPath && data.path) {
          elements.configPath.textContent = data.path;
        }
        if (elements.apiKeyInput && currentConfig.api_keys && currentConfig.api_keys.length > 0) {
          elements.apiKeyInput.value = currentConfig.api_keys[0];
          if (elements.quickApiKey) elements.quickApiKey.textContent = currentConfig.api_keys[0];
        }
        if (elements.promptTuningInput && currentConfig.system_prompt_tuning) {
          elements.promptTuningInput.value = currentConfig.system_prompt_tuning;
        }

        // Kilo configs
        const kModel = currentConfig.kilo_default_model || 'openrouter/free';
        if (elements.kiloModelSelect) elements.kiloModelSelect.value = kModel;
        if (elements.kiloTestModelSelect) elements.kiloTestModelSelect.value = kModel;
        if (elements.quickKiloModelId) elements.quickKiloModelId.textContent = kModel;
        renderKiloChips(kModel);

        // Puter configs
        const pModel = currentConfig.puter_default_model || 'claude-opus-5-5';
        if (elements.puterModelSelect) elements.puterModelSelect.value = pModel;
        if (elements.puterTestModelSelect) elements.puterTestModelSelect.value = pModel;
        if (elements.quickPuterModelId) elements.quickPuterModelId.textContent = pModel;
        renderPuterChips(pModel);

        syncModelSelect();
      }
    } catch (e) {
      console.warn('Failed to load config', e);
    }
  }

  const knownFreeDirect = new Set([
    'big-pickle',
    'default'
  ]);

  function isFreeModel(id) {
    if (!id) return false;
    const s = id.toLowerCase();
    return s.endsWith('-free') || 
           s.includes('-free-') || 
           s.includes('contributor-free') || 
           s.includes('preview-free') || 
           knownFreeDirect.has(s);
  }

  // Load /v1/models (supports real-time force refresh)
  async function loadModels(force = false) {
    try {
      const url = force ? '/v1/models?refresh=1' : '/v1/models';
      const res = await fetch(url);
      if (res.ok) {
        const json = await res.json();
        const newModels = (json.data || []).map(m => m.id);
        const prevSelect = elements.modelSelect ? elements.modelSelect.value : null;
        const prevTestSelect = elements.testModelSelect ? elements.testModelSelect.value : null;

        const hasChanged = JSON.stringify(newModels) !== JSON.stringify(availableModels);
        if (hasChanged || force || availableModels.length === 0) {
          availableModels = newModels;
          renderModelOptions();
          if (prevSelect && availableModels.includes(prevSelect)) {
            elements.modelSelect.value = prevSelect;
          }
          if (prevTestSelect && availableModels.includes(prevTestSelect)) {
            elements.testModelSelect.value = prevTestSelect;
          }
        }
      }
    } catch (e) {
      console.warn('Failed to load models', e);
    }
  }

  function renderModelOptions() {
    if (!elements.modelSelect || !elements.testModelSelect) return;
    elements.modelSelect.innerHTML = '';
    elements.testModelSelect.innerHTML = '';
    elements.modelChips.innerHTML = '';

    const freeModels = availableModels.filter(isFreeModel);
    const otherModels = availableModels.filter(m => !isFreeModel(m));

    const populateDropdown = (selectEl) => {
      if (freeModels.length > 0) {
        const groupFree = document.createElement('optgroup');
        groupFree.label = `⚡ Auto-Detected Free Models (${freeModels.length})`;
        freeModels.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          groupFree.appendChild(opt);
        });
        selectEl.appendChild(groupFree);
      }

      if (otherModels.length > 0) {
        const groupOther = document.createElement('optgroup');
        groupOther.label = `🌐 Upstream Models (${otherModels.length})`;
        otherModels.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          groupOther.appendChild(opt);
        });
        selectEl.appendChild(groupOther);
      }
    };

    populateDropdown(elements.modelSelect);
    populateDropdown(elements.testModelSelect);

    freeModels.forEach(m => {
      const chip = document.createElement('span');
      chip.className = 'chip';
      chip.textContent = m;
      chip.title = `Pilih model: ${m}`;
      chip.onclick = () => {
        elements.modelSelect.value = m;
        elements.testModelSelect.value = m;
        if (elements.quickModelId) elements.quickModelId.textContent = m;
        document.querySelectorAll('#model-chips .chip').forEach(c => c.classList.remove('active'));
        chip.classList.add('active');
      };
      elements.modelChips.appendChild(chip);
    });

    syncModelSelect();
  }

  function syncModelSelect() {
    if (currentConfig && currentConfig.default_model) {
      elements.modelSelect.value = currentConfig.default_model;
      elements.testModelSelect.value = currentConfig.default_model;
      if (elements.quickModelId) {
        elements.quickModelId.textContent = currentConfig.default_model;
      }
      document.querySelectorAll('#model-chips .chip').forEach(c => {
        if (c.textContent === currentConfig.default_model) {
          c.classList.add('active');
        } else {
          c.classList.remove('active');
        }
      });
    }
  }

  // Update quick display & chips when dropdown changes
  elements.modelSelect.onchange = () => {
    const val = elements.modelSelect.value;
    elements.testModelSelect.value = val;
    if (elements.quickModelId) elements.quickModelId.textContent = val;
    document.querySelectorAll('#model-chips .chip').forEach(c => {
      if (c.textContent === val) c.classList.add('active');
      else c.classList.remove('active');
    });
  };

  if (elements.puterModelSelect) {
    elements.puterModelSelect.onchange = () => {
      const val = elements.puterModelSelect.value;
      if (elements.puterTestModelSelect) elements.puterTestModelSelect.value = val;
      if (elements.quickPuterModelId) elements.quickPuterModelId.textContent = val;
      document.querySelectorAll('#puter-model-chips .chip').forEach(c => {
        if (c.textContent === val) c.classList.add('active');
        else c.classList.remove('active');
      });
    };
  }

  // Connect Refresh models button
  if (elements.btnRefreshModels) {
    elements.btnRefreshModels.onclick = async () => {
      elements.btnRefreshModels.disabled = true;
      const originalText = elements.btnRefreshModels.textContent;
      elements.btnRefreshModels.textContent = '🔄 Loading...';
      try {
        await loadModels(true);
      } finally {
        elements.btnRefreshModels.disabled = false;
        elements.btnRefreshModels.textContent = originalText;
      }
    };
  }

  // Connect Copy buttons
  if (elements.btnCopyModel) {
    elements.btnCopyModel.onclick = () => copyToClipboard(elements.modelSelect.value, elements.btnCopyModel);
  }
  if (elements.btnCopyTestModel) {
    elements.btnCopyTestModel.onclick = () => copyToClipboard(elements.testModelSelect.value, elements.btnCopyTestModel);
  }
  if (elements.btnCopyQuickModel) {
    elements.btnCopyQuickModel.onclick = () => copyToClipboard(elements.quickModelId ? elements.quickModelId.textContent : elements.modelSelect.value, elements.btnCopyQuickModel);
  }
  if (elements.btnCopyBaseUrl) {
    elements.btnCopyBaseUrl.onclick = () => copyToClipboard(elements.quickBaseUrl ? elements.quickBaseUrl.textContent : 'http://127.0.0.1:20144/v1', elements.btnCopyBaseUrl);
  }
  if (elements.btnCopyApiKey) {
    elements.btnCopyApiKey.onclick = () => copyToClipboard(elements.quickApiKey ? elements.quickApiKey.textContent : 'sk-opencode-free', elements.btnCopyApiKey);
  }
  if (elements.btnCopyKiloModel) {
    elements.btnCopyKiloModel.onclick = () => copyToClipboard(elements.kiloModelSelect.value, elements.btnCopyKiloModel);
  }
  if (elements.btnCopyKiloTestModel) {
    elements.btnCopyKiloTestModel.onclick = () => copyToClipboard(elements.kiloTestModelSelect.value, elements.btnCopyKiloTestModel);
  }
  if (elements.btnCopyKiloBaseUrl) {
    elements.btnCopyKiloBaseUrl.onclick = () => copyToClipboard(elements.quickKiloBaseUrl ? elements.quickKiloBaseUrl.textContent : 'http://127.0.0.1:20144/v2/kilo', elements.btnCopyKiloBaseUrl);
  }
  if (elements.btnCopyQuickKiloModel) {
    elements.btnCopyQuickKiloModel.onclick = () => copyToClipboard(elements.quickKiloModelId ? elements.quickKiloModelId.textContent : 'openrouter/free', elements.btnCopyQuickKiloModel);
  }
  if (elements.btnCopyPuterModel) {
    elements.btnCopyPuterModel.onclick = () => copyToClipboard(elements.puterModelSelect.value, elements.btnCopyPuterModel);
  }
  if (elements.btnCopyPuterTestModel) {
    elements.btnCopyPuterTestModel.onclick = () => copyToClipboard(elements.puterTestModelSelect.value, elements.btnCopyPuterTestModel);
  }
  if (elements.btnCopyPuterBaseUrl) {
    elements.btnCopyPuterBaseUrl.onclick = () => copyToClipboard(elements.quickPuterBaseUrl ? elements.quickPuterBaseUrl.textContent : 'http://127.0.0.1:20144/v2/puter', elements.btnCopyPuterBaseUrl);
  }
  if (elements.btnCopyQuickPuterModel) {
    elements.btnCopyQuickPuterModel.onclick = () => copyToClipboard(elements.quickPuterModelId ? elements.quickPuterModelId.textContent : 'claude-3-5-sonnet', elements.btnCopyQuickPuterModel);
  }
  if (elements.btnCopyV2BaseUrl) {
    elements.btnCopyV2BaseUrl.onclick = () => copyToClipboard(elements.quickV2BaseUrl ? elements.quickV2BaseUrl.textContent : 'http://127.0.0.1:20144/v2', elements.btnCopyV2BaseUrl);
  }

  // Save OpenCode settings (Tab 1)
  elements.saveModelBtn.onclick = async () => {
    if (!currentConfig) return;
    const selected = elements.modelSelect.value;
    elements.saveModelBtn.disabled = true;
    elements.saveModelBtn.textContent = 'Saving...';

    const nextConfig = { 
      ...currentConfig, 
      default_model: selected,
      system_prompt_tuning: elements.promptTuningInput ? elements.promptTuningInput.value.trim() : '',
      puter_default_model: elements.puterModelSelect ? elements.puterModelSelect.value : (currentConfig.puter_default_model || 'claude-opus-5-5'),
      puter_enabled: true
    };

    try {
      const res = await fetch('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(nextConfig)
      });
      if (res.ok) {
        const saved = await res.json();
        currentConfig = saved.config;
        elements.defaultModelBadge.textContent = currentConfig.default_model;
        if (elements.quickModelId) elements.quickModelId.textContent = currentConfig.default_model;
        alert(`Settings saved! Default model: ${selected}`);
      } else {
        alert('Failed to save config: ' + res.statusText);
      }
    } catch (e) {
      alert('Error updating config: ' + e.message);
    } finally {
      elements.saveModelBtn.disabled = false;
      elements.saveModelBtn.textContent = 'Save Settings';
    }
  };

  // Save Kilo Default Model
  if (elements.btnSaveKiloModel) {
    elements.btnSaveKiloModel.onclick = async () => {
      if (!currentConfig) return;
      const model = elements.kiloModelSelect ? elements.kiloModelSelect.value : 'openrouter/free';
      elements.btnSaveKiloModel.disabled = true;
      elements.btnSaveKiloModel.textContent = 'Menyimpan...';

      const nextConfig = {
        ...currentConfig,
        kilo_default_model: model,
        kilo_enabled: true
      };

      try {
        const res = await fetch('/api/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(nextConfig)
        });
        if (res.ok) {
          const saved = await res.json();
          currentConfig = saved.config;
          if (elements.quickKiloModelId) elements.quickKiloModelId.textContent = model;
          alert(`✓ Model default Kilo berhasil disimpan: ${model}`);
        } else {
          alert('Gagal menyimpan config: ' + res.statusText);
        }
      } catch (e) {
        alert('Error saving Kilo model: ' + e.message);
      } finally {
        elements.btnSaveKiloModel.disabled = false;
        elements.btnSaveKiloModel.textContent = '💾 Simpan Model Default Kilo';
      }
    };
  }

  // Save Puter Default Model
  if (elements.btnSavePuterModel) {
    elements.btnSavePuterModel.onclick = async () => {
      if (!currentConfig) return;
      const model = elements.puterModelSelect ? elements.puterModelSelect.value : 'claude-opus-5-5';
      elements.btnSavePuterModel.disabled = true;
      elements.btnSavePuterModel.textContent = 'Menyimpan...';

      const nextConfig = {
        ...currentConfig,
        puter_default_model: model,
        puter_enabled: true
      };

      try {
        const res = await fetch('/api/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(nextConfig)
        });
        if (res.ok) {
          const saved = await res.json();
          currentConfig = saved.config;
          if (elements.quickPuterModelId) elements.quickPuterModelId.textContent = model;
          alert(`✓ Model default Puter berhasil disimpan: ${model}`);
        } else {
          alert('Gagal menyimpan config: ' + res.statusText);
        }
      } catch (e) {
        alert('Error saving Puter model: ' + e.message);
      } finally {
        elements.btnSavePuterModel.disabled = false;
        elements.btnSavePuterModel.textContent = '💾 Simpan Model Default Puter';
      }
    };
  }

  // OpenCode Test playground (Streaming SSE)
  elements.btnTest.onclick = async () => {
    const model = elements.testModelSelect.value;
    const prompt = elements.testPrompt.value.trim();
    if (!prompt) return;

    elements.btnTest.disabled = true;
    elements.testOutput.textContent = 'Connecting to upstream & streaming response...\n';
    elements.testStats.textContent = '';
    const start = performance.now();

    try {
      const res = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${elements.apiKeyInput.value || 'sk-opencode-free'}`
        },
        body: JSON.stringify({
          model: model,
          stream: true,
          messages: [{ role: 'user', content: prompt }]
        })
      });

      if (!res.ok) {
        const err = await res.text();
        elements.testOutput.textContent = `HTTP ${res.status} Error:\n${err}`;
        elements.btnTest.disabled = false;
        return;
      }

      elements.testOutput.textContent = '';
      const reader = res.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let fullText = '';
      let reasoningText = '';
      let buffer = '';
      let streamModel = model;
      let streamUsage = null;

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data: ')) {
            const raw = trimmed.slice(6).trim();
            if (raw === '[DONE]') continue;
            try {
              const parsed = JSON.parse(raw);
              if (parsed.model && streamModel === model) {
                streamModel = parsed.model;
              }
              if (parsed.usage) {
                streamUsage = parsed.usage;
              }
              const delta = parsed.choices?.[0]?.delta;
              if (delta?.reasoning_content) {
                reasoningText += delta.reasoning_content;
              }
              if (delta?.content) {
                fullText += delta.content;
              }
              elements.testOutput.textContent = (reasoningText ? `[Reasoning]\n${reasoningText}\n\n[Answer]\n` : '') + fullText;
              elements.testOutput.scrollTop = elements.testOutput.scrollHeight;
            } catch (_) {}
          }
        }
      }

      const elapsed = Math.round(performance.now() - start);
      let promptTok = streamUsage ? (streamUsage.prompt_tokens || 0) : 0;
      let cachedTok = streamUsage?.prompt_tokens_details?.cached_tokens || 0;
      let outTok = streamUsage ? (streamUsage.completion_tokens || 0) : (fullText.trim().split(/\s+/).filter(Boolean).length || 0);
      let hitPct = promptTok > 0 ? ((cachedTok / promptTok) * 100).toFixed(1) : '0';

      let statsText = `⚡ Model: ${streamModel} | Out: ${outTok} tk | Latency: ${elapsed} ms`;
      if (promptTok > 0) {
        statsText = `⚡ Model: ${streamModel} | In: ${promptTok.toLocaleString()} tk (${hitPct}% cache hit / ${cachedTok.toLocaleString()} cached) | Out: ${outTok.toLocaleString()} tk | Latency: ${elapsed} ms`;
      }
      elements.testStats.textContent = statsText;

      loadHealth();
      loadTelemetry();
    } catch (e) {
      elements.testOutput.textContent = 'Stream error: ' + e.message;
    } finally {
      elements.btnTest.disabled = false;
    }
  };

  // Kilo Test playground (Streaming SSE via /v2/kilo/chat/completions)
  if (elements.btnKiloTest) {
    elements.btnKiloTest.onclick = async () => {
      const model = elements.kiloTestModelSelect ? elements.kiloTestModelSelect.value : 'openrouter/free';
      const prompt = elements.kiloTestPrompt ? elements.kiloTestPrompt.value.trim() : '';
      if (!prompt) return;

      elements.btnKiloTest.disabled = true;
      elements.kiloTestStats.textContent = '';
      const start = performance.now();

      elements.kiloTestOutput.textContent = `Menghubungi model ${model} via Kilo Gateway...\n`;
      try {
        const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
        const res = await fetch('/v2/kilo/chat/completions', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${apiKey}`
          },
          body: JSON.stringify({
            model: model,
            stream: true,
            messages: [{ role: 'user', content: prompt }]
          })
        });

        if (!res.ok) {
          const err = await res.text();
          elements.kiloTestOutput.textContent = `HTTP ${res.status} Error:\n${err}`;
          elements.btnKiloTest.disabled = false;
          return;
        }

        elements.kiloTestOutput.textContent = '';
        const reader = res.body.getReader();
        const decoder = new TextDecoder('utf-8');
        let fullText = '';
        let reasoningText = '';
        let buffer = '';
        let streamModel = model;

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop();

          for (const line of lines) {
            const trimmed = line.trim();
            if (trimmed.startsWith('data: ')) {
              const raw = trimmed.slice(6).trim();
              if (raw === '[DONE]') continue;
              try {
                const parsed = JSON.parse(raw);
                if (parsed.model && streamModel === model) {
                  streamModel = parsed.model;
                }
                const delta = parsed.choices?.[0]?.delta;
                if (delta?.reasoning || delta?.reasoning_content) {
                  reasoningText += (delta.reasoning || delta.reasoning_content);
                }
                if (delta?.content) {
                  fullText += delta.content;
                }
                elements.kiloTestOutput.textContent = (reasoningText ? `[Reasoning]\n${reasoningText}\n\n[Answer]\n` : '') + fullText;
                elements.kiloTestOutput.scrollTop = elements.kiloTestOutput.scrollHeight;
              } catch (_) {}
            }
          }
        }

        const elapsed = Math.round(performance.now() - start);
        elements.kiloTestStats.textContent = `🟢 Kilo Model: ${streamModel} | Status: Completed | Latency: ${elapsed} ms`;

        loadHealth();
        loadTelemetry();
      } catch (e) {
        elements.kiloTestOutput.textContent = 'Kilo stream error: ' + e.message;
      } finally {
        elements.btnKiloTest.disabled = false;
      }
    };
  }

  // Puter Test playground (Streaming SSE via Puter.js Client or /v2/puter/chat/completions)
  if (elements.btnPuterTest) {
    elements.btnPuterTest.onclick = async () => {
      const model = elements.puterTestModelSelect ? elements.puterTestModelSelect.value : 'claude-3-5-sonnet';
      const prompt = elements.puterTestPrompt ? elements.puterTestPrompt.value.trim() : '';
      if (!prompt) return;

      elements.btnPuterTest.disabled = true;
      elements.puterTestStats.textContent = '';
      const start = performance.now();

      // Priority 1: Jika sudah login via Puter.js browser OAuth, gunakan kredit gratis 1,000 tanpa batas REST subscription!
      if (window.puter && puter.auth && puter.auth.isSignedIn()) {
        elements.puterTestOutput.textContent = `Menghubungi model ${model} via Puter.js (1,000 Kredit Bulanan Gratis)...\n`;
        try {
          const streamResp = await puter.ai.chat(prompt, { model: model, stream: true });
          elements.puterTestOutput.textContent = '';
          let fullText = '';
          for await (const part of streamResp) {
            const chunk = part?.text || (typeof part === 'string' ? part : '');
            fullText += chunk;
            elements.puterTestOutput.textContent = fullText;
            elements.puterTestOutput.scrollTop = elements.puterTestOutput.scrollHeight;
          }
          const elapsed = Math.round(performance.now() - start);
          elements.puterTestStats.textContent = `🟣 Model: ${model} | Jalur: Puter.js Client (1,000 Free Credits) | Latensi: ${elapsed} ms`;
          return;
        } catch (streamErr) {
          try {
            const directResp = await puter.ai.chat(prompt, { model: model });
            const reply = typeof directResp === 'object' ? (directResp.message?.content || JSON.stringify(directResp)) : directResp;
            elements.puterTestOutput.textContent = reply;
            const elapsed = Math.round(performance.now() - start);
            elements.puterTestStats.textContent = `🟣 Model: ${model} | Jalur: Puter.js Client (1,000 Free Credits) | Latensi: ${elapsed} ms`;
            return;
          } catch (fallbackErr) {
            elements.puterTestOutput.textContent = `Puter.js Error: ${fallbackErr.message || JSON.stringify(fallbackErr)}`;
            return;
          }
        } finally {
          elements.btnPuterTest.disabled = false;
        }
      }

      // Priority 2: Via Local Go Proxy (/v2/puter/chat/completions)
      elements.puterTestOutput.textContent = 'Menghubungi Puter AI via Go Proxy Gateway...\n';
      try {
        const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
        const res = await fetch('/v2/puter/chat/completions', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${apiKey}`
          },
          body: JSON.stringify({
            model: model,
            stream: true,
            messages: [{ role: 'user', content: prompt }]
          })
        });

        if (!res.ok) {
          const err = await res.text();
          if (res.status === 402) {
            elements.puterTestOutput.textContent = `HTTP 402 Error: A subscription is required for this action.\n\n` +
              `💡 UPSTREAM REST API MEMERLUKAN LANGGANAN:\n` +
              `Endpoint REST API resmi (api.puter.com) mewajibkan paket berbayar jika menggunakan API Key direct.\n\n` +
              `🎉 SOLUSI KREDIT GRATIS (1,000 MONTHLY CREDITS):\n` +
              `Puter memberikan 1,000 kredit gratis bulanan untuk akun Anda jika diakses via browser library!\n` +
              `Silakan klik tombol '🔑 Login Popup Puter' di atas. Setelah login popup, playground ini akan otomatis menggunakan 1,000 kredit gratis Anda tanpa perlu langganan!`;
          } else {
            elements.puterTestOutput.textContent = `HTTP ${res.status} Error:\n${err}`;
          }
          elements.btnPuterTest.disabled = false;
          return;
        }

        elements.puterTestOutput.textContent = '';
        const reader = res.body.getReader();
        const decoder = new TextDecoder('utf-8');
        let fullText = '';
        let reasoningText = '';
        let buffer = '';
        let streamModel = model;

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop();

          for (const line of lines) {
            const trimmed = line.trim();
            if (trimmed.startsWith('data: ')) {
              const raw = trimmed.slice(6).trim();
              if (raw === '[DONE]') continue;
              try {
                const parsed = JSON.parse(raw);
                if (parsed.model && streamModel === model) {
                  streamModel = parsed.model;
                }
                const delta = parsed.choices?.[0]?.delta;
                if (delta?.reasoning_content) {
                  reasoningText += delta.reasoning_content;
                }
                if (delta?.content) {
                  fullText += delta.content;
                }
                elements.puterTestOutput.textContent = (reasoningText ? `[Reasoning]\n${reasoningText}\n\n[Answer]\n` : '') + fullText;
                elements.puterTestOutput.scrollTop = elements.puterTestOutput.scrollHeight;
              } catch (_) {}
            }
          }
        }

        const elapsed = Math.round(performance.now() - start);
        elements.puterTestStats.textContent = `🟣 Puter Model: ${streamModel} | Status: Completed | Latency: ${elapsed} ms`;

        loadHealth();
        loadTelemetry();
      } catch (e) {
        elements.puterTestOutput.textContent = 'Puter stream error: ' + e.message;
      } finally {
        elements.btnPuterTest.disabled = false;
      }
    };
  }

  if (elements.btnRefreshTelemetry) {
    elements.btnRefreshTelemetry.onclick = loadTelemetry;
  }

  if (elements.btnClearTelemetry) {
    elements.btnClearTelemetry.onclick = async () => {
      const ok = confirm('Kosongkan semua data telemetri di memori (RAM) dan riwayat log?');
      if (!ok) return;

      elements.btnClearTelemetry.disabled = true;
      try {
        const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
        const res = await fetch('/api/telemetry/clear', {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${apiKey}`
          }
        });
        if (res.ok) {
          loadHealth();
          loadTelemetry();
        } else {
          alert('Gagal membersihkan data: ' + res.statusText);
        }
      } catch (err) {
        alert('Error: ' + err.message);
      } finally {
        elements.btnClearTelemetry.disabled = false;
      }
    };
  }

  // ==========================================
  // Puter Browser Relay Bridge (SSE + HTTP)
  // ==========================================
  let bridgeEvtSource = null;
  let bridgeReconnectTimer = null;
  let bridgeServedCount = 0;

  const relayDot = document.getElementById('relay-status-dot');
  const relayBadge = document.getElementById('relay-status-badge');
  const relayDesc = document.getElementById('relay-status-desc');
  const relayAccount = document.getElementById('relay-account-name');
  const relayCount = document.getElementById('relay-stats-count');
  const btnLogin = document.getElementById('btn-puter-login');
  const btnSwitch = document.getElementById('btn-puter-switch');
  const btnLogout = document.getElementById('btn-puter-logout');

  function updateBridgeUI(status, username) {
    if (relayCount) {
      relayCount.textContent = `${bridgeServedCount} Request`;
    }

    if (status === 'active') {
      if (relayDot) {
        relayDot.style.background = '#10b981';
        relayDot.style.boxShadow = '0 0 10px #10b981';
      }
      if (relayBadge) {
        relayBadge.className = 'badge-puter-status active';
        relayBadge.textContent = 'Aktif & Siap Melayani';
      }
      if (relayDesc) {
        relayDesc.innerHTML = 'Puter AI kini 100% menggunakan <strong>Browser Relay SDK (puter.js)</strong> langsung di browser Anda. Tab ini siap melayani request DeepSeek Harness / Cline via <strong>1,000 kredit gratis per bulan</strong>!';
      }
      if (relayAccount) {
        relayAccount.textContent = username || 'Akun Terautentikasi';
        relayAccount.style.color = '#34d399';
      }
      if (btnLogin) btnLogin.style.display = 'none';
      if (btnSwitch) btnSwitch.style.display = 'inline-flex';
      if (btnLogout) btnLogout.style.display = 'inline-flex';
    } else if (status === 'warning') {
      if (relayDot) {
        relayDot.style.background = '#f59e0b';
        relayDot.style.boxShadow = '0 0 10px #f59e0b';
      }
      if (relayBadge) {
        relayBadge.className = 'badge-puter-status';
        relayBadge.textContent = 'Perlu Login Puter';
      }
      if (relayDesc) {
        relayDesc.textContent = 'Tab browser terhubung ke Go proxy, tetapi belum login ke Puter. Silakan klik "🔑 Login ke Puter (Popup)" untuk mengaktifkan 1,000 kredit gratis!';
      }
      if (relayAccount) {
        relayAccount.textContent = 'Belum Login';
        relayAccount.style.color = '#f59e0b';
      }
      if (btnLogin) btnLogin.style.display = 'inline-flex';
      if (btnSwitch) btnSwitch.style.display = 'none';
      if (btnLogout) btnLogout.style.display = 'none';
    } else {
      if (relayDot) {
        relayDot.style.background = '#ef4444';
        relayDot.style.boxShadow = '0 0 10px #ef4444';
      }
      if (relayBadge) {
        relayBadge.className = 'badge-puter-status';
        relayBadge.textContent = 'Terputus';
      }
      if (relayDesc) {
        relayDesc.textContent = 'Koneksi ke Go Proxy terputus. Mencoba menghubungkan ulang...';
      }
      if (relayAccount) {
        relayAccount.textContent = 'Tidak Terhubung';
        relayAccount.style.color = '#ef4444';
      }
      if (btnLogin) btnLogin.style.display = 'inline-flex';
      if (btnSwitch) btnSwitch.style.display = 'none';
      if (btnLogout) btnLogout.style.display = 'none';
    }
  }

  function normalizeMessagesForPuter(messages) {
    if (typeof messages === 'string') return messages;
    if (!Array.isArray(messages)) return String(messages);

    return messages.map(m => {
      let content = m.content;
      if (Array.isArray(content)) {
        content = content.map(part => part.text || part.image_url?.url || JSON.stringify(part)).join('\n');
      }

      const role = m.role || 'user';
      let strContent = content != null ? String(content) : '';

      // Multi-turn tool calling adaptation for Claude / Puter:
      // 1. Tool execution result message: Claude doesn't accept role="tool"
      if (role === 'tool') {
        const toolLabel = m.name || m.tool_call_id || 'tool';
        return {
          role: 'user',
          content: `[Tool Result: ${toolLabel}]:\n${strContent || '(no output)'}`
        };
      }

      // 2. Assistant message with tool_calls: Claude requires non-empty content
      if (role === 'assistant' && strContent.trim() === '' && Array.isArray(m.tool_calls) && m.tool_calls.length > 0) {
        strContent = m.tool_calls.map(tc => {
          const fnName = tc.function?.name || 'tool';
          const fnArgs = tc.function?.arguments || '';
          return `<invoke name="${fnName}">\n${fnArgs}\n</invoke>`;
        }).join('\n');
      }

      // 3. Fallback: ensure content is never pure whitespace if assistant
      if (role === 'assistant' && strContent.trim() === '') {
        strContent = '(empty assistant response)';
      }

      return {
        role: role,
        content: strContent
      };
    });
  }

  async function notifyBridgeStatus() {
    const isSignedIn = window.puter && puter.auth && puter.auth.isSignedIn();
    let username = '';
    if (isSignedIn) {
      try {
        const u = await puter.auth.getUser();
        username = u?.username || u?.email || 'authenticated_user';
      } catch (_) {}
    }

    try {
      await fetch('/api/browser-relay/ready', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          signed_in: Boolean(isSignedIn),
          username: username
        })
      });
    } catch (_) {}

    if (isSignedIn) {
      updateBridgeUI('active', username);
    } else {
      updateBridgeUI('warning');
    }
  }

  async function postBridgeChunk(id, text) {
    try {
      await fetch('/api/browser-relay/chunk', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, text })
      });
    } catch (_) {}
  }

  async function postBridgeDone(id) {
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const res = await fetch('/api/browser-relay/done', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id })
        });
        if (res.ok) return;
      } catch (e) {
        console.warn(`[BrowserBridge] postBridgeDone retry ${attempt + 1}:`, e);
      }
      await new Promise(r => setTimeout(r, 150 * (attempt + 1)));
    }
  }

  async function postBridgeError(id, error) {
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const res = await fetch('/api/browser-relay/error', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id, error: String(error) })
        });
        if (res.ok) return;
      } catch (e) {
        console.warn(`[BrowserBridge] postBridgeError retry ${attempt + 1}:`, e);
      }
      await new Promise(r => setTimeout(r, 150 * (attempt + 1)));
    }
  }

  async function handleBridgeChatJob(job) {
    const { id, model, messages, stream } = job;
    try {
      if (!window.puter || !puter.ai || !puter.auth || !puter.auth.isSignedIn()) {
        await postBridgeError(id, 'Browser Relay: Tab browser belum login ke Puter. Buka tab Puter AI Hub dan klik login popup.');
        return;
      }

      const puterInput = normalizeMessagesForPuter(messages);

      // Enforce 100s timeout so background tabs or stale streams never hang indefinitely
      const callTimeoutMs = 100000;
      const timeoutPromise = new Promise((_, reject) => {
        setTimeout(() => reject(new Error(`Puter AI execution timed out after ${callTimeoutMs / 1000}s`)), callTimeoutMs);
      });

      if (stream) {
        try {
          const streamResp = await Promise.race([
            puter.ai.chat(puterInput, { model: model, stream: true }),
            timeoutPromise
          ]);
          for await (const part of streamResp) {
            const chunkText = part?.text || (typeof part === 'string' ? part : '');
            if (chunkText) {
              await postBridgeChunk(id, chunkText);
            }
          }
          await postBridgeDone(id);
        } catch (streamErr) {
          console.warn('[BrowserBridge] Primary stream failed, attempting prompt fallback:', streamErr);
          // Fallback: join to plain string prompt
          const textPrompt = Array.isArray(messages)
            ? messages.map(m => `${(m.role || 'user').toUpperCase()}: ${typeof m.content === 'string' ? m.content : JSON.stringify(m.content)}`).join('\n\n')
            : String(messages);
          const streamResp2 = await Promise.race([
            puter.ai.chat(textPrompt, { model: model, stream: true }),
            timeoutPromise
          ]);
          for await (const part of streamResp2) {
            const chunkText = part?.text || (typeof part === 'string' ? part : '');
            if (chunkText) {
              await postBridgeChunk(id, chunkText);
            }
          }
          await postBridgeDone(id);
        }
      } else {
        const directResp = await Promise.race([
          puter.ai.chat(puterInput, { model: model }),
          timeoutPromise
        ]);
        const text = typeof directResp === 'object' ? (directResp.message?.content || JSON.stringify(directResp)) : String(directResp);
        await postBridgeChunk(id, text);
        await postBridgeDone(id);
      }

      bridgeServedCount++;
      notifyBridgeStatus();
    } catch (err) {
      console.error('[BrowserBridge] Chat job error:', err);
      await postBridgeError(id, err.message || String(err));
    }
  }

  function initBrowserBridge() {
    function connect() {
      if (bridgeEvtSource) {
        try { bridgeEvtSource.close(); } catch (_) {}
      }

      bridgeEvtSource = new EventSource('/api/browser-relay/events');

      bridgeEvtSource.onopen = () => {
        console.log('[BrowserBridge] SSE connected to Go Proxy');
        notifyBridgeStatus();
      };

      bridgeEvtSource.addEventListener('chat', async (evt) => {
        try {
          const job = JSON.parse(evt.data);
          await handleBridgeChatJob(job);
        } catch (err) {
          console.error('[BrowserBridge] Error parsing chat job:', err);
        }
      });

      bridgeEvtSource.onerror = () => {
        updateBridgeUI('error', 'Terputus dari Proxy', 'Mencoba menghubungkan ulang ke Go proxy...');
        try { bridgeEvtSource.close(); } catch (_) {}
        scheduleReconnect();
      };
    }

    function scheduleReconnect() {
      if (bridgeReconnectTimer) return;
      bridgeReconnectTimer = setTimeout(() => {
        bridgeReconnectTimer = null;
        connect();
      }, 3000);
    }

    connect();

    // Re-verify login status periodically & on tab focus
    setInterval(() => {
      notifyBridgeStatus();
    }, 8000);

    window.addEventListener('focus', () => notifyBridgeStatus());
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        notifyBridgeStatus();
      }
    });
  }

  // WARP IPv6 Fallback Controls (Tab 1)
  if (elements.btnCopyWarpIP) {
    elements.btnCopyWarpIP.onclick = () => {
      const ip = lastWarpStatus && lastWarpStatus.warp ? lastWarpStatus.warp.active_ip : '';
      if (ip) copyToClipboard(ip, elements.btnCopyWarpIP);
    };
  }

  if (elements.warpToggleEnabled) {
    elements.warpToggleEnabled.onchange = async () => {
      if (!currentConfig) return;
      const enabled = Boolean(elements.warpToggleEnabled.checked);
      elements.warpToggleEnabled.disabled = true;
      try {
        const res = await fetch('/api/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ...currentConfig, warp_fallback_enabled: enabled })
        });
        if (res.ok) {
          const saved = await res.json();
          currentConfig = saved.config;
          await loadWarpStatus();
        } else {
          elements.warpToggleEnabled.checked = !enabled;
          alert('Gagal mengubah status WARP Fallback: ' + res.statusText);
        }
      } catch (e) {
        elements.warpToggleEnabled.checked = !enabled;
        alert('Error updating WARP Fallback: ' + e.message);
      } finally {
        elements.warpToggleEnabled.disabled = false;
      }
    };
  }

  if (elements.btnWarpHop) {
    elements.btnWarpHop.onclick = async () => {
      const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
      elements.btnWarpHop.disabled = true;
      const origText = elements.btnWarpHop.textContent;
      elements.btnWarpHop.textContent = '⏳ Hopping IPv6...';
      try {
        const res = await fetch('/api/warp/hop', {
          method: 'POST',
          headers: { 'Authorization': `Bearer ${apiKey}` }
        });
        const data = await res.json();
        if (!res.ok) {
          alert('Hop IPv6 gagal: ' + (data?.error?.message || res.statusText));
        } else if (data.warp_status) {
          lastWarpFetchedAt = Date.now();
          renderWarpStatus(data.warp_status);
        } else {
          await loadWarpStatus();
        }
      } catch (e) {
        alert('Error saat hop IPv6: ' + e.message);
      } finally {
        elements.btnWarpHop.disabled = false;
        elements.btnWarpHop.textContent = origText;
      }
    };
  }

  if (elements.btnWarpStop) {
    elements.btnWarpStop.onclick = async () => {
      const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
      elements.btnWarpStop.disabled = true;
      try {
        const res = await fetch('/api/warp/stop', {
          method: 'POST',
          headers: { 'Authorization': `Bearer ${apiKey}` }
        });
        const data = await res.json();
        if (res.ok && data.warp_status) {
          lastWarpFetchedAt = Date.now();
          renderWarpStatus(data.warp_status);
        } else {
          await loadWarpStatus();
        }
        await loadHealth();
      } catch (e) {
        alert('Error saat stop WARP: ' + e.message);
      } finally {
        elements.btnWarpStop.disabled = false;
      }
    };
  }

  if (elements.btnWarpClearBurned) {
    elements.btnWarpClearBurned.onclick = async () => {
      const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
      elements.btnWarpClearBurned.disabled = true;
      try {
        const res = await fetch('/api/warp/burned/clear', {
          method: 'POST',
          headers: { 'Authorization': `Bearer ${apiKey}` }
        });
        const data = await res.json();
        if (res.ok && data.warp_status) {
          lastWarpFetchedAt = Date.now();
          renderWarpStatus(data.warp_status);
        } else {
          await loadWarpStatus();
        }
      } catch (e) {
        alert('Error saat clear burned list: ' + e.message);
      } finally {
        elements.btnWarpClearBurned.disabled = false;
      }
    };
  }

  // Initial loads
  initBrowserBridge();
  loadHealth();
  loadConfig();
  loadWarpStatus();
  loadModels();
  loadKiloModels();
  loadPuterModels();
  loadTelemetry();

  // Smooth 1-second live countdown ticker for direct reset, WARP idle timer, and burned /64 list
  setInterval(() => {
    if (lastWarpStatus) {
      renderWarpStatus(lastWarpStatus);
    }
  }, 1000);

  // Poll health and telemetry every 4s
  setInterval(() => {
    loadHealth();
    loadTelemetry();
  }, 4000);

  // Poll models every 8s in background to keep preset chips & list updated realtime
  setInterval(() => {
    loadModels();
  }, 8000);

  // Poll Kilo catalog every 60s
  setInterval(() => {
    loadKiloModels();
  }, 60000);

  // Poll Puter catalog every 60s
  setInterval(() => {
    loadPuterModels();
  }, 60000);
});
