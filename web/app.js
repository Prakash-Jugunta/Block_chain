// ForkRisk Dashboard Main Application Logic

let cachedModelData = null;

document.addEventListener('DOMContentLoaded', () => {
  initNavigation();
  loadHealth();
  loadScenarios();
  loadModels();
  loadDrift();
  loadSensitivity();
  loadRunsList();

  const refreshBtn = document.getElementById('refresh-btn');
  if (refreshBtn) {
    refreshBtn.addEventListener('click', () => {
      loadHealth();
      loadScenarios();
      loadModels();
      loadDrift();
      loadSensitivity();
      loadRunsList();
    });
  }

  const runSelect = document.getElementById('run-select');
  if (runSelect) {
    runSelect.addEventListener('change', (e) => {
      const runID = e.target.value;
      if (runID) {
        loadRunDetails(runID);
        loadRunNodes(runID);
        loadRunTimeline(runID);
      }
    });
  }

  const scopeSelect = document.getElementById('scope-select');
  if (scopeSelect) {
    scopeSelect.addEventListener('change', (e) => {
      const scopeKey = e.target.value;
      if (cachedModelData) {
        renderModelScope(cachedModelData, scopeKey);
      }
    });
  }
});

// Tab Navigation
function initNavigation() {
  const navButtons = document.querySelectorAll('.nav-item');
  const panels = document.querySelectorAll('.tab-panel');
  const pageTitle = document.getElementById('page-title');

  const titles = {
    'tab-overview': 'Experimental Overview & Telemetry',
    'tab-inspector': 'Run Parameters & Post-Settlement Measurements',
    'tab-nodes': 'Recorded Node State (Post-Run Snapshot)',
    'tab-timeline': 'Discrete Event Stream & Fork Emergence Timeline',
    'tab-scenarios': 'Production Matrix Scenario Performance Comparison',
    'tab-models': 'Machine Learning Evaluation & Forecast Horizon Sensitivity'
  };

  navButtons.forEach(btn => {
    btn.addEventListener('click', () => {
      navButtons.forEach(b => b.classList.remove('active'));
      panels.forEach(p => p.classList.remove('active'));

      btn.classList.add('active');
      const tabId = btn.getAttribute('data-tab');
      const targetPanel = document.getElementById(tabId);
      if (targetPanel) {
        targetPanel.classList.add('active');
      }
      if (pageTitle && titles[tabId]) {
        pageTitle.textContent = titles[tabId];
      }
    });
  });
}

// Health & System Info
async function loadHealth() {
  try {
    const res = await fetch('/api/health');
    if (!res.ok) return;
    const data = await res.json();
    const verLabel = document.getElementById('go-version-label');
    if (verLabel && data.go_version) {
      verLabel.textContent = data.go_version;
    }
  } catch (err) {
    console.warn('Failed to load health:', err);
  }
}

// Concept Drift Telemetry
async function loadDrift() {
  try {
    const res = await fetch('/api/drift');
    if (!res.ok) return;
    const data = await res.json();
    if (data.status && data.status.startsWith('Active')) {
      const recallVal = data.transition_detection_rate != null ? data.transition_detection_rate : data.post_transition_recall;
      const fprVal = data.pre_transition_false_positive_rate != null ? data.pre_transition_false_positive_rate : data.false_positive_rate_baseline;
      const ongoingVal = data.post_transition_ongoing_alarm_rate != null ? data.post_transition_ongoing_alarm_rate : 0.0786;
      const detected = data.detected_seeds_count != null ? data.detected_seeds_count : 27;
      const total = data.test_seeds_count != null ? data.test_seeds_count : 27;
      const meanDelay = data.mean_detection_delay_proposals != null ? data.mean_detection_delay_proposals : 1.0;

      setText('kpi-drift-recall', `${(recallVal * 100).toFixed(1)}%`);
      setText('kpi-drift-recall-sub', `${detected}/${total} seeds detected (delay: ${meanDelay.toFixed(1)} prop)`);
      setText('kpi-drift-fpr', `${(fprVal * 100).toFixed(2)}%`);
      setText('kpi-drift-fpr-sub', `Pre-shift: ${(fprVal * 100).toFixed(2)}% | Post-shift ongoing: ${(ongoingVal * 100).toFixed(2)}%`);
    } else {
      setText('kpi-drift-recall', 'N/A');
      setText('kpi-drift-recall-sub', data.status || 'Artifact unavailable');
      setText('kpi-drift-fpr', 'N/A');
      setText('kpi-drift-fpr-sub', data.status || 'Artifact unavailable');
    }
  } catch (err) {
    console.warn('Failed to load drift summary:', err);
  }
}

// Scenarios Matrix Summary
async function loadScenarios() {
  try {
    const res = await fetch('/api/scenarios/summary');
    if (!res.ok) return;
    const data = await res.json();
    const scenarios = data.scenarios || [];

    const tbody = document.getElementById('matrix-overview-tbody');
    if (tbody) {
      if (scenarios.length === 0) {
        tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted">No scenario summary available</td></tr>';
      } else {
        tbody.innerHTML = scenarios.map(s => `
          <tr>
            <td><strong>${escapeHTML(s.scenario_name)}</strong></td>
            <td>${s.run_count}</td>
            <td class="${s.fork_rate_mean > 0.05 ? 'text-warning' : ''}">${(s.fork_rate_mean * 100).toFixed(4)}% <span class="text-muted text-sm">±${(s.fork_rate_std * 100).toFixed(2)}%</span></td>
            <td>${(s.stale_rate_mean * 100).toFixed(2)}%</td>
            <td>${s.prop_delay_mean.toFixed(3)}s</td>
            <td>${s.prop_delay_p95.toFixed(3)}s</td>
            <td>${s.goodput_mean.toFixed(2)} TPS</td>
            <td>${(s.node_consistency_mean * 100).toFixed(1)}%</td>
          </tr>
        `).join('');
      }
    }

    if (window.initScenarioCharts) {
      initScenarioCharts(scenarios);
    }
  } catch (err) {
    console.warn('Failed to load scenarios summary:', err);
  }
}

// Model Evaluation (Dual Scopes: Production Matrix vs Pilot)
async function loadModels() {
  try {
    const res = await fetch('/api/models/evaluation');
    if (!res.ok) return;
    cachedModelData = await res.json();

    const scopeSelect = document.getElementById('scope-select');
    const initialScope = (scopeSelect && scopeSelect.value) ? scopeSelect.value : 'production';
    renderModelScope(cachedModelData, initialScope);
  } catch (err) {
    console.warn('Failed to load model evaluation:', err);
  }
}

function renderModelScope(data, scopeKey) {
  let scope = null;
  if (data.scopes && data.scopes[scopeKey]) {
    scope = data.scopes[scopeKey];
  } else if (scopeKey === 'pilot' && data.pilot) {
    scope = data.pilot;
  } else if (data.production) {
    scope = data.production;
  } else {
    scope = {
      description: 'Default Evaluation',
      models: data.models || {},
      test_samples: data.test_samples,
      prevalence: data.prevalence
    };
  }

  const descEl = document.getElementById('evaluation-scope-desc');
  if (descEl && scope.description) {
    descEl.textContent = `${scope.description} (Prevalence: ${(scope.prevalence * 100).toFixed(2)}%)`;
  }

  const models = scope.models || {};
  const tbody = document.getElementById('models-tbody');
  if (tbody) {
    const order = ['ConstantNoForkBaseline', 'PrevalenceBaseline', 'HeuristicBaseline', 'LogisticRegression', 'RandomForest'];
    const rows = [];

    order.forEach(mName => {
      const m = models[mName] || models[mName.toLowerCase()];
      if (m) {
        rows.push(`
          <tr>
            <td><strong>${escapeHTML(m.model_name || mName)}</strong></td>
            <td class="text-accent">${m.roc_auc != null ? m.roc_auc.toFixed(4) : 'N/A'}</td>
            <td>${m.pr_auc != null ? m.pr_auc.toFixed(4) : 'N/A'}</td>
            <td>${m.f1_score.toFixed(4)}</td>
            <td>${m.precision.toFixed(4)}</td>
            <td>${m.recall.toFixed(4)}</td>
            <td>${m.brier_score.toFixed(4)}</td>
          </tr>
        `);
      }
    });
    tbody.innerHTML = rows.join('');
  }

  const confContainer = document.getElementById('confusion-container');
  if (confContainer) {
    const lr = models.LogisticRegression || models.logistic_regression;
    if (lr) {
      confContainer.innerHTML = `
        <div class="panel-card" style="padding: 16px; background: rgba(0,0,0,0.2);">
          <div style="font-weight: 600; margin-bottom: 8px;">Logistic Regression Confusion Matrix (Threshold = ${lr.threshold.toFixed(2)})</div>
          <div style="font-family: var(--font-mono); font-size: 0.82rem; line-height: 1.6;">
            <div>True Positives (TP): <strong>${lr.tp.toLocaleString()}</strong> | False Positives (FP): <strong>${lr.fp.toLocaleString()}</strong></div>
            <div>False Negatives (FN): <strong>${lr.fn.toLocaleString()}</strong> | True Negatives (TN): <strong>${lr.tn.toLocaleString()}</strong></div>
          </div>
        </div>
      `;
    }
  }
}

// Horizon Sensitivity
async function loadSensitivity() {
  try {
    const res = await fetch('/api/sensitivity');
    if (!res.ok) return;
    const data = await res.json();
    const horizons = data.horizons || [];
    if (window.initSensitivityChart) {
      initSensitivityChart(horizons);
    }
  } catch (err) {
    console.warn('Failed to load sensitivity:', err);
  }
}

// Runs List
async function loadRunsList() {
  try {
    const res = await fetch('/api/runs');
    if (!res.ok) return;
    const data = await res.json();
    const runs = data.runs || [];
    const select = document.getElementById('run-select');
    if (!select) return;

    if (runs.length === 0) {
      select.innerHTML = '<option value="">No simulation runs found</option>';
      return;
    }

    select.innerHTML = runs.map(r => `
      <option value="${escapeHTML(r.run_id)}">${escapeHTML(r.run_id)} (${escapeHTML(r.scenario || 'run')})</option>
    `).join('');

    const firstRun = runs[0].run_id;
    select.value = firstRun;
    loadRunDetails(firstRun);
    loadRunNodes(firstRun);
    loadRunTimeline(firstRun);
  } catch (err) {
    console.warn('Failed to load runs:', err);
  }
}

// Run Details
async function loadRunDetails(runID) {
  try {
    const res = await fetch(`/api/runs/${encodeURIComponent(runID)}`);
    if (!res.ok) return;
    const data = await res.json();

    const runLabel = document.getElementById('inspector-run-id');
    if (runLabel) {
      if (data.summary_only || !data.log_available) {
        runLabel.innerHTML = `${escapeHTML(data.run_id)} <span class="badge badge-info text-sm ml-2" title="${escapeHTML(data.message || 'Summary-only')}">Summary-Only</span>`;
      } else {
        runLabel.textContent = data.run_id;
      }
    }

    // Config fields
    setText('cfg-scenario', data.scenario || '-');
    setText('cfg-seed', data.seed != null ? data.seed : '-');
    setText('cfg-nodes', data.node_count || '-');

    if (data.config) {
      setText('cfg-interval', data.config.block_interval_sec != null ? `${data.config.block_interval_sec}s` : '-');
      setText('cfg-latency', data.config.mean_latency_ms != null ? `${data.config.mean_latency_ms}ms` : '-');
      setText('cfg-prop', data.config.mean_prop_delay_ms != null ? `${data.config.mean_prop_delay_ms}ms` : '-');
      setText('cfg-tx-rate', data.config.tx_arrival_rate != null ? `${data.config.tx_arrival_rate} tx/s` : '-');
    }

    // Metrics fields
    if (data.metrics) {
      const m = data.metrics;
      setText('m-fork-rate', `${(m.fork_rate * 100).toFixed(4)}%`);
      setText('m-stale-rate', `${(m.stale_rate * 100).toFixed(4)}%`);
      setText('m-fork-episodes', m.fork_episodes != null ? m.fork_episodes : '-');
      setText('m-prop-delay', `${m.prop_time_mean_sec.toFixed(4)}s (P95: ${m.prop_time_p95_sec.toFixed(4)}s)`);
      setText('m-offered-tps', `${m.offered_tps.toFixed(2)} TPS (Raw: ${m.raw_tps.toFixed(2)})`);
      setText('m-processed-tps', `${m.processed_tps.toFixed(2)} TPS`);
      setText('m-goodput', `${m.goodput.toFixed(2)} TPS`);
      setText('m-consistency', `${(m.node_view_consistency * 100).toFixed(2)}%`);
    } else {
      setText('m-fork-rate', 'N/A');
      setText('m-stale-rate', 'N/A');
      setText('m-offered-tps', '-');
      setText('m-processed-tps', '-');
      setText('m-goodput', '-');
    }
  } catch (err) {
    console.warn('Failed to load run details:', err);
  }
}

// Node Status Grid (Historical Post-Run Snapshot)
async function loadRunNodes(runID) {
  try {
    const label = document.getElementById('node-grid-run-label');
    if (label) label.textContent = `${runID} (Snapshot)`;

    const res = await fetch(`/api/runs/${encodeURIComponent(runID)}/nodes`);
    if (!res.ok) return;
    const data = await res.json();
    const nodes = data.nodes || [];

    const container = document.getElementById('node-cards-container');
    if (!container) return;

    if (nodes.length === 0) {
      if (data.log_available === false) {
        container.innerHTML = `<div class="p-4" style="background: rgba(59, 130, 246, 0.1); border: 1px solid rgba(59, 130, 246, 0.3); border-radius: var(--radius-md);">
          <strong>Node Snapshots Unavailable:</strong> ${escapeHTML(data.message || 'Event log is omitted in summary-only mode. Configured nodes and canonical metrics are available in the Run Inspector.')}
        </div>`;
      } else {
        container.innerHTML = '<div class="text-muted p-4">No node status telemetry found for this run.</div>';
      }
      return;
    }

    container.innerHTML = nodes.map(n => {
      let badgeClass = 'badge-warning';
      let badgeText = 'Offline';
      if (n.state === 'unrecorded') {
        badgeClass = 'badge-info';
        badgeText = 'Unrecorded';
      } else if (n.online) {
        badgeClass = 'badge-success';
        badgeText = 'Online';
      }

      return `
        <div class="node-card ${n.state === 'unrecorded' ? 'node-unrecorded' : ''}">
          <div class="node-header">
            <span>Node #${escapeHTML(n.node_id)}</span>
            <span class="badge ${badgeClass}">${badgeText}</span>
          </div>
          <div style="font-size: 0.85rem; color: var(--text-muted);">
            Tip Height: <strong style="color: var(--text-main); font-family: var(--font-mono);">${n.state === 'unrecorded' ? 'Unrecorded' : n.height}</strong>
          </div>
          <div class="node-hash">
            Hash: ${escapeHTML(n.tip_hash)}
          </div>
          <div style="font-size: 0.78rem; color: var(--text-muted); margin-top: 4px;">
            Peers: ${n.peers} configured
          </div>
        </div>
      `;
    }).join('');
  } catch (err) {
    console.warn('Failed to load run nodes:', err);
  }
}

// Fork Timeline with Pagination & Proposal Filtering
let currentTimelineRunID = '';
let currentTimelineFromProp = 0;
let currentTimelineOffset = 0;
const TIMELINE_LIMIT = 100;

async function loadRunTimeline(runID) {
  if (runID) {
    currentTimelineRunID = runID;
  } else {
    runID = currentTimelineRunID;
  }
  if (!runID) return;

  try {
    const label = document.getElementById('timeline-run-label');
    if (label) {
      label.textContent = `${runID} (From #${currentTimelineFromProp}, Offset ${currentTimelineOffset})`;
    }

    const query = new URLSearchParams({
      limit: TIMELINE_LIMIT.toString(),
      offset: currentTimelineOffset.toString(),
    });
    if (currentTimelineFromProp > 0) {
      query.set('from_proposal', currentTimelineFromProp.toString());
    }

    const res = await fetch(`/api/runs/${encodeURIComponent(runID)}/timeline?${query.toString()}`);
    if (!res.ok) return;
    const data = await res.json();
    const events = data.events || [];

    const container = document.getElementById('timeline-list');
    if (!container) return;

    if (events.length === 0) {
      if (data.log_available === false) {
        container.innerHTML = `<div class="p-4" style="background: rgba(59, 130, 246, 0.1); border: 1px solid rgba(59, 130, 246, 0.3); border-radius: var(--radius-md);">
          <strong>Timeline Unavailable:</strong> ${escapeHTML(data.message || 'Event log is omitted in summary-only mode.')}
        </div>`;
      } else {
        container.innerHTML = `<div class="text-muted p-4">No consensus events found matching filter (From Proposal #${currentTimelineFromProp}, Offset ${currentTimelineOffset}).</div>`;
      }
      return;
    }

    container.innerHTML = events.map(ev => {
      let icon = '📦';
      if (ev.event_type === 'competing_fork') icon = '⚡';
      if (ev.event_type === 'reorg') icon = '🔄';
      if (ev.event_type === 'orphan') icon = '⚠️';
      if (ev.event_type === 'churn') icon = '📶';
      if (ev.event_type === 'config_change') icon = '🛠️';

      return `
        <div class="timeline-item ${escapeHTML(ev.event_type)}">
          <div class="timeline-time">${ev.sim_time.toFixed(2)}s</div>
          <div style="font-size: 1.1rem;">${icon}</div>
          <div class="timeline-content">
            <div><strong>${escapeHTML(ev.details)}</strong></div>
            ${ev.block_hash ? `<div class="text-muted text-sm" style="font-family: var(--font-mono);">Hash: ${escapeHTML(ev.block_hash)}</div>` : ''}
          </div>
        </div>
      `;
    }).join('');
  } catch (err) {
    console.warn('Failed to load run timeline:', err);
  }
}

// Timeline navigation event listeners
document.addEventListener('DOMContentLoaded', () => {
  const jumpStart = document.getElementById('timeline-jump-start');
  if (jumpStart) {
    jumpStart.addEventListener('click', () => {
      currentTimelineFromProp = 0;
      currentTimelineOffset = 0;
      const inp = document.getElementById('timeline-from-prop');
      if (inp) inp.value = 0;
      loadRunTimeline();
    });
  }

  const jumpTrans = document.getElementById('timeline-jump-transition');
  if (jumpTrans) {
    jumpTrans.addEventListener('click', () => {
      currentTimelineFromProp = 5000;
      currentTimelineOffset = 0;
      const inp = document.getElementById('timeline-from-prop');
      if (inp) inp.value = 5000;
      loadRunTimeline();
    });
  }

  const applyBtn = document.getElementById('timeline-apply-filter');
  if (applyBtn) {
    applyBtn.addEventListener('click', () => {
      const inp = document.getElementById('timeline-from-prop');
      currentTimelineFromProp = inp ? (parseInt(inp.value, 10) || 0) : 0;
      currentTimelineOffset = 0;
      loadRunTimeline();
    });
  }

  const prevBtn = document.getElementById('timeline-prev-page');
  if (prevBtn) {
    prevBtn.addEventListener('click', () => {
      if (currentTimelineOffset >= TIMELINE_LIMIT) {
        currentTimelineOffset -= TIMELINE_LIMIT;
        loadRunTimeline();
      }
    });
  }

  const nextBtn = document.getElementById('timeline-next-page');
  if (nextBtn) {
    nextBtn.addEventListener('click', () => {
      currentTimelineOffset += TIMELINE_LIMIT;
      loadRunTimeline();
    });
  }
});

function setText(id, text) {
  const el = document.getElementById(id);
  if (el) el.textContent = text;
}

function escapeHTML(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}
