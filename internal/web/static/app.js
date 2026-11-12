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
    
    // Tab 3: Puter AI
    poolAccountsCount: document.getElementById('pool-accounts-count'),
    btnAddAccountPopup: document.getElementById('btn-add-account-popup'),
    btnToggleManualForm: document.getElementById('btn-toggle-manual-form'),
    formManualAccount: document.getElementById('form-manual-account'),
    manualAccUsername: document.getElementById('manual-acc-username'),
    manualAccToken: document.getElementById('manual-acc-token'),
    btnSubmitManualAccount: document.getElementById('btn-submit-manual-account'),
    tbodyPuterAccounts: document.getElementById('tbody-puter-accounts'),
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

  // Puter token eye toggle
  if (elements.btnTogglePuterToken && elements.puterTokenInput) {
    elements.btnTogglePuterToken.onclick = () => {
      const isPwd = elements.puterTokenInput.type === 'password';
      elements.puterTokenInput.type = isPwd ? 'text' : 'password';
      elements.btnTogglePuterToken.textContent = isPwd ? '🙈' : '👁️';
    };
  }

  // Puter.js Multi-Account Pool Management
  let puterAccounts = [];

  async function loadPuterAccounts() {
    if (!elements.tbodyPuterAccounts) return;
    try {
      const res = await fetch('/api/puter/accounts');
      if (!res.ok) return;
      const data = await res.json();
      puterAccounts = data.accounts || [];

      if (elements.poolAccountsCount) {
        elements.poolAccountsCount.textContent = `${puterAccounts.length} Akun`;
      }

      if (puterAccounts.length === 0) {
        elements.tbodyPuterAccounts.innerHTML = `
          <tr>
            <td colspan="5" style="text-align: center; color: var(--text-muted); padding: 24px;">
              Belum ada akun Puter di dalam pool. Klik <strong>🔑 Tambah Akun (Popup Login)</strong> untuk menambahkan akun dan memakai 1,000 kredit bulanan gratis!
            </td>
          </tr>
        `;
        return;
      }

      elements.tbodyPuterAccounts.innerHTML = '';
      let foundPrimary = false;

      puterAccounts.forEach((acc, idx) => {
        const row = document.createElement('tr');
        
        let statusBadge = '';
        const isCooldown = acc.status === 'cooldown';
        const isRateLimited = acc.status === 'rate_limited';

        if (acc.status === 'active') {
          if (!foundPrimary) {
            statusBadge = `<span class="badge-pool-primary">🟢 PRIMARY (In-Use)</span>`;
            foundPrimary = true;
          } else {
            statusBadge = `<span class="badge-pool-standby">🛡️ STANDBY (Failover #${idx})</span>`;
          }
        } else if (isCooldown) {
          statusBadge = `<span class="badge-pool-exhausted" title="${escapeHtml(acc.last_error || 'Kredit habis')}">🔴 KREDIT HABIS (Cooldown)</span>`;
        } else if (isRateLimited) {
          statusBadge = `<span class="badge-pool-ratelimit" title="${escapeHtml(acc.last_error || 'Rate limit')}">⏳ RATE LIMITED (60s)</span>`;
        } else {
          statusBadge = `<span class="badge-puter-status">${escapeHtml(acc.status)}</span>`;
        }

        const lastUsedStr = acc.last_used_at ? formatTime(acc.last_used_at) : '<span style="color: var(--text-muted);">-</span>';
        
        const setPrimaryBtn = (idx > 0 && acc.status === 'active')
          ? `<button class="btn-action-small" onclick="window.setPuterPrimary('${escapeHtml(acc.id)}')">⭐ Set Primary</button>`
          : '';

        const resetBtn = (acc.status !== 'active')
          ? `<button class="btn-action-small" onclick="window.resetPuterAccount('${escapeHtml(acc.id)}')">↻ Reset</button>`
          : '';

        row.innerHTML = `
          <td style="font-weight: 700; color: var(--accent-purple);">#${acc.priority || (idx + 1)}</td>
          <td>
            <div style="font-weight: 600; color: #fff;">${escapeHtml(acc.username || 'Puter User')}</div>
            <div style="font-size: 10px; color: var(--text-muted); font-family: var(--font-mono);">${escapeHtml(acc.id)}</div>
          </td>
          <td>${statusBadge}</td>
          <td style="font-size: 11px;">${lastUsedStr}</td>
          <td style="text-align: right; display: flex; gap: 5px; justify-content: flex-end; align-items: center;">
            ${setPrimaryBtn}
            <button class="btn-action-small" onclick="window.pingPuterAccount('${escapeHtml(acc.id)}')">⚡ Ping</button>
            ${resetBtn}
            <button class="btn-action-small btn-action-danger" onclick="window.deletePuterAccount('${escapeHtml(acc.id)}', '${escapeHtml(acc.username)}')">🗑️</button>
          </td>
        `;
        elements.tbodyPuterAccounts.appendChild(row);
      });
    } catch (e) {
      console.warn('Failed to load puter accounts', e);
    }
  }

  // Global window helpers for table action buttons
  window.setPuterPrimary = async (id) => {
    try {
      const res = await fetch(`/api/puter/accounts/priority?id=${encodeURIComponent(id)}&priority=1`, { method: 'POST' });
      if (res.ok) {
        await loadPuterAccounts();
      } else {
        alert('Gagal mengubah prioritas akun: ' + res.statusText);
      }
    } catch (err) {
      alert('Error: ' + err.message);
    }
  };

  window.resetPuterAccount = async (id) => {
    try {
      const res = await fetch(`/api/puter/accounts/reset?id=${encodeURIComponent(id)}`, { method: 'POST' });
      if (res.ok) {
        await loadPuterAccounts();
      } else {
        alert('Gagal me-reset akun: ' + res.statusText);
      }
    } catch (err) {
      alert('Error: ' + err.message);
    }
  };

  window.deletePuterAccount = async (id, username) => {
    if (!confirm(`Hapus akun '${username}' dari pool Puter?`)) return;
    try {
      const res = await fetch(`/api/puter/accounts?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (res.ok) {
        await loadPuterAccounts();
      } else {
        alert('Gagal menghapus akun: ' + res.statusText);
      }
    } catch (err) {
      alert('Error: ' + err.message);
    }
  };

  window.pingPuterAccount = async (id) => {
    try {
      const model = elements.puterModelSelect ? elements.puterModelSelect.value : 'claude-3-5-haiku';
      const apiKey = (elements.apiKeyInput && elements.apiKeyInput.value) ? elements.apiKeyInput.value : 'sk-opencode-free';
      const res = await fetch('/api/puter/ping', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${apiKey}`
        },
        body: JSON.stringify({ account_id: id, model })
      });
      const data = await res.json();
      if (data.status === 'ok') {
        alert(`✓ Akun Terhubung & Valid!\nModel: ${data.model}\nLatensi: ${data.latency_ms} ms`);
      } else {
        alert(`✗ Hasil Ping: ${data.message || 'Error'}`);
      }
      await loadPuterAccounts();
    } catch (err) {
      alert('Ping error: ' + err.message);
    }
  };

  // Popup OAuth Add Account
  if (elements.btnAddAccountPopup) {
    elements.btnAddAccountPopup.onclick = async () => {
      try {
        elements.btnAddAccountPopup.disabled = true;
        elements.btnAddAccountPopup.textContent = 'Membuka popup login...';

        if (window.puter && puter.auth) {
          // Sign out first to allow logging in with another account
          try { await puter.auth.signOut(); } catch (_) {}
          
          await puter.auth.signIn();
          const user = await puter.auth.getUser();
          const token = puter.authToken;

          if (!token) {
            alert('Tidak dapat mendeteksi auth token Puter setelah login.');
            return;
          }

          const username = user?.username || user?.email || `user_${Date.now() % 10000}`;
          const res = await fetch('/api/puter/accounts', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              username: username,
              token: token,
              status: 'active'
            })
          });

          if (res.ok) {
            await loadPuterAccounts();
            alert(`✓ Berhasil menambahkan akun '${username}' ke pool Puter!`);
          } else {
            alert('Gagal menyimpan akun ke pool: ' + res.statusText);
          }
        } else {
          alert('Puter.js SDK belum siap dimuat di browser.');
        }
      } catch (err) {
        console.warn('Popup login error or canceled', err);
      } finally {
        elements.btnAddAccountPopup.disabled = false;
        elements.btnAddAccountPopup.textContent = '🔑 Tambah Akun (Popup Login)';
      }
    };
  }

  // Toggle Manual Account Form
  if (elements.btnToggleManualForm && elements.formManualAccount) {
    elements.btnToggleManualForm.onclick = () => {
      const isHidden = elements.formManualAccount.style.display === 'none';
      elements.formManualAccount.style.display = isHidden ? 'block' : 'none';
    };
  }

  // Submit Manual Account
  if (elements.btnSubmitManualAccount) {
    elements.btnSubmitManualAccount.onclick = async () => {
      const username = elements.manualAccUsername ? elements.manualAccUsername.value.trim() : '';
      const token = elements.manualAccToken ? elements.manualAccToken.value.trim() : '';
      if (!token) {
        alert('Auth token Puter tidak boleh kosong!');
        return;
      }
      elements.btnSubmitManualAccount.disabled = true;
      elements.btnSubmitManualAccount.textContent = 'Menyimpan...';

      try {
        const res = await fetch('/api/puter/accounts', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            username: username || `acc_${Date.now() % 10000}`,
            token: token,
            status: 'active'
          })
        });
        if (res.ok) {
          if (elements.manualAccUsername) elements.manualAccUsername.value = '';
          if (elements.manualAccToken) elements.manualAccToken.value = '';
          elements.formManualAccount.style.display = 'none';
          await loadPuterAccounts();
          alert('✓ Akun manual berhasil ditambahkan ke pool!');
        } else {
          alert('Gagal menambahkan akun manual: ' + res.statusText);
        }
      } catch (err) {
        alert('Error: ' + err.message);
      } finally {
        elements.btnSubmitManualAccount.disabled = false;
        elements.btnSubmitManualAccount.textContent = '💾 Simpan ke Pool';
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
          item.style.cssText = 'display: flex; justify-content: space-between; align-items: center; background: var(--code-bg); padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border); font-family: var(--font-mono); font-size: 11px;';
          const isUp = p.status === 'active';
          const statusColor = isUp ? 'var(--accent-green)' : 'var(--danger)';
          const label = p.address === 'direct' ? '🌐 Direct (Local Machine Egress)' : `🛡️ Proxy: ${p.address}`;
          item.innerHTML = `
            <span style="color: ${isUp ? '#fff' : 'var(--text-muted)'};">${escapeHtml(label)}</span>
            <span style="color: ${statusColor}; font-weight: 600;">${p.status.toUpperCase()}${p.cooldown_remaining_sec > 0 ? ` (${p.cooldown_remaining_sec}s cd)` : ''}</span>
          `;
          elements.proxyList.appendChild(item);
        });
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
    'claude-opus-5',
    'claude-haiku-5-5',
    'claude-fable-5',
    'gpt-4o',
    'gpt-4o-mini',
    'deepseek-v4.1-flash'
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
    const gptList = models.filter(m => m.toLowerCase().includes('gpt'));
    const deepseekList = models.filter(m => m.toLowerCase().includes('deepseek') || m.toLowerCase().includes('qwen'));
    const otherList = models.filter(m => !claudeList.includes(m) && !gptList.includes(m) && !deepseekList.includes(m));

    const populate = (selectEl) => {
      selectEl.innerHTML = '';
      if (claudeList.length > 0) {
        const g = document.createElement('optgroup');
        g.label = `🌟 Anthropic Claude Models (${claudeList.length})`;
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
        g.label = `⚡ DeepSeek & Open Models (${deepseekList.length})`;
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
        g.label = `🌐 All Other Puter Models (${otherList.length})`;
        otherList.slice(0, 100).forEach(m => {
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
        if (elements.puterTokenInput) {
          elements.puterTokenInput.value = currentConfig.puter_api_key || '';
        }
        if (elements.puterStatusBadge) {
          if (currentConfig.puter_api_key) {
            elements.puterStatusBadge.className = 'badge-puter-status active';
            elements.puterStatusBadge.textContent = 'Configured';
          } else {
            elements.puterStatusBadge.className = 'badge-puter-status';
            elements.puterStatusBadge.textContent = 'Not Configured';
          }
        }
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
      puter_api_key: elements.puterTokenInput ? elements.puterTokenInput.value.trim() : (currentConfig.puter_api_key || ''),
      puter_default_model: elements.puterModelSelect ? elements.puterModelSelect.value : (currentConfig.puter_default_model || 'claude-3-5-sonnet'),
      puter_enabled: elements.puterTokenInput ? !!elements.puterTokenInput.value.trim() : (currentConfig.puter_enabled || false)
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

  // Initial loads
  loadHealth();
  loadConfig();
  loadModels();
  loadKiloModels();
  loadPuterModels();
  loadTelemetry();
  loadPuterAccounts();

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
