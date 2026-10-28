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
    quickModelId: document.getElementById('quick-model-id'),
    quickBaseUrl: document.getElementById('quick-base-url'),
    quickApiKey: document.getElementById('quick-api-key'),
    btnCopyQuickModel: document.getElementById('btn-copy-quick-model'),
    btnCopyBaseUrl: document.getElementById('btn-copy-base-url'),
    btnCopyApiKey: document.getElementById('btn-copy-api-key')
  };

  let currentConfig = null;
  let availableModels = [];

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
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // Load /health
  async function loadHealth() {
    try {
      const res = await fetch('/health');
      if (res.ok) {
        const data = await res.json();
        elements.uptime.textContent = formatUptime(data.uptime_seconds);
        elements.requests.textContent = data.requests || 0;
        elements.sessions.textContent = data.sessions || 0;
        elements.defaultModelBadge.textContent = data.default_model || '-';

        if (elements.proxyList && data.proxies) {
          elements.proxyList.innerHTML = '';
          data.proxies.forEach(p => {
            const row = document.createElement('div');
            row.style.cssText = 'display: flex; align-items: center; justify-content: space-between; background: var(--code-bg); padding: 7px 12px; border-radius: 6px; border: 1px solid var(--border); font-family: var(--font-mono); font-size: 12px;';
            const statusBadge = p.healthy 
              ? '<span style="color: var(--accent-green); display: flex; align-items: center; gap: 5px;"><span style="display:inline-block; width:7px; height:7px; border-radius:50%; background:var(--accent-green);"></span> Ready</span>' 
              : `<span style="color: var(--danger); display: flex; align-items: center; gap: 5px;"><span style="display:inline-block; width:7px; height:7px; border-radius:50%; background:var(--danger);"></span> Cooldown (${p.cooldown_seconds_left}s)</span>`;
            row.innerHTML = `<span style="color: #cbd5e1; font-weight: 500;">${p.url}</span> ${statusBadge}`;
            elements.proxyList.appendChild(row);
          });
        }
      }
    } catch (e) {
      console.warn('Health check failed', e);
    }
  }

  // Load /api/telemetry
  async function loadTelemetry() {
    try {
      const res = await fetch('/api/telemetry');
      if (!res.ok) return;
      const data = await res.json();

      if (elements.inputTokens) {
        elements.inputTokens.textContent = formatTokens(data.total_prompt_tokens);
      }
      if (elements.outputTokens) {
        elements.outputTokens.textContent = formatTokens(data.total_output_tokens);
      }
      const hitPct = data.overall_cache_hit_pct || 0;
      if (elements.cacheBarFill) {
        elements.cacheBarFill.style.width = Math.min(100, Math.max(0, hitPct)) + '%';
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

            row.innerHTML = `
              <td style="color: var(--text-muted);">${formatTime(r.timestamp)}</td>
              <td>${modelHtml}</td>
              <td><span class="lane-tag">${escapeHtml(r.lane || 'chat')}</span></td>
              <td style="font-weight: 600;">${(r.prompt_tokens || 0).toLocaleString()}</td>
              <td><span class="pill-hit ${pillClass}">${r.cache_hit_rate || 0}% (${formatTokens(r.cached_tokens || 0)})</span></td>
              <td>${(r.output_tokens || 0).toLocaleString()}</td>
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

  // Helper to dynamically detect free-tier models (including future models from opencode.ai)
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
          syncModelSelect();
        }
      }
    } catch (e) {
      console.warn('Failed to load models', e);
    }
  }

  function renderModelOptions() {
    elements.modelSelect.innerHTML = '';
    elements.testModelSelect.innerHTML = '';
    elements.modelChips.innerHTML = '';

    const freeModels = availableModels.filter(isFreeModel);
    const otherModels = availableModels.filter(m => !isFreeModel(m));

    // Priority sort for well-known models on top
    const priority = [
      'big-pickle',
      'mimo-v2.6-flash-free',
      'nemotron-3.5-lightning-free',
      'glm-5.3-flash-free',
      'beam-free',
      'flint-40b-free',
      'nemotron-3.5-super-vl-free',
      'jev-1.13-free',
      'muse-spark-1.3-contributor-free',
      'muse-spark-1.2-contributor-free',
      'deepseek-v4.1-flash-free'
    ];

    freeModels.sort((a, b) => {
      const ia = priority.indexOf(a);
      const ib = priority.indexOf(b);
      if (ia !== -1 && ib !== -1) return ia - ib;
      if (ia !== -1) return -1;
      if (ib !== -1) return 1;
      return a.localeCompare(b);
    });

    otherModels.sort((a, b) => a.localeCompare(b));

    // Populate dropdown with clean optgroups
    const populateDropdown = (selectEl) => {
      selectEl.innerHTML = '';
      if (freeModels.length > 0) {
        const grpFree = document.createElement('optgroup');
        grpFree.label = `✨ Free-Tier Models (${freeModels.length} Auto-Detected)`;
        freeModels.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          grpFree.appendChild(opt);
        });
        selectEl.appendChild(grpFree);
      }
      if (otherModels.length > 0) {
        const grpOther = document.createElement('optgroup');
        grpOther.label = `🌐 All Upstream Models (${otherModels.length} Models)`;
        otherModels.forEach(m => {
          const opt = document.createElement('option');
          opt.value = m;
          opt.textContent = m;
          grpOther.appendChild(opt);
        });
        selectEl.appendChild(grpOther);
      }
    };

    populateDropdown(elements.modelSelect);
    populateDropdown(elements.testModelSelect);

    // Render chips for ALL dynamically discovered free models
    freeModels.forEach(m => {
      const chip = document.createElement('span');
      chip.className = 'chip';
      chip.textContent = m;
      chip.title = `Pilih model: ${m}`;
      chip.onclick = () => {
        elements.modelSelect.value = m;
        elements.testModelSelect.value = m;
        if (elements.quickModelId) elements.quickModelId.textContent = m;
        document.querySelectorAll('.chip').forEach(c => c.classList.remove('active'));
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
      document.querySelectorAll('.chip').forEach(c => {
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
    document.querySelectorAll('.chip').forEach(c => {
      if (c.textContent === val) c.classList.add('active');
      else c.classList.remove('active');
    });
  };

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

  // Save settings
  elements.saveModelBtn.onclick = async () => {
    if (!currentConfig) return;
    const selected = elements.modelSelect.value;
    elements.saveModelBtn.disabled = true;
    elements.saveModelBtn.textContent = 'Saving...';

    const nextConfig = { 
      ...currentConfig, 
      default_model: selected,
      system_prompt_tuning: elements.promptTuningInput ? elements.promptTuningInput.value.trim() : ''
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

  // Test playground (Streaming SSE)
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
        buffer = lines.pop(); // keep incomplete line in buffer

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

      if (buffer.trim().startsWith('data: ')) {
        const raw = buffer.trim().slice(6).trim();
        if (raw !== '[DONE]') {
          try {
            const parsed = JSON.parse(raw);
            if (parsed.model && streamModel === model) streamModel = parsed.model;
            if (parsed.usage) streamUsage = parsed.usage;
            const delta = parsed.choices?.[0]?.delta;
            if (delta?.content) {
              fullText += delta.content;
              elements.testOutput.textContent = (reasoningText ? `[Reasoning]\n${reasoningText}\n\n[Answer]\n` : '') + fullText;
            }
          } catch (_) {}
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

      // Immediately refresh stats & telemetry
      loadHealth();
      loadTelemetry();
    } catch (e) {
      elements.testOutput.textContent = 'Stream error: ' + e.message;
    } finally {
      elements.btnTest.disabled = false;
    }
  };

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
  loadTelemetry();

  // Poll health and telemetry every 4s
  setInterval(() => {
    loadHealth();
    loadTelemetry();
  }, 4000);

  // Poll models every 8s in background to keep preset chips & list updated realtime
  setInterval(() => {
    loadModels();
  }, 8000);
});
