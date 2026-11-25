"use strict";

const $ = id => document.getElementById(id);
const setText = (id, value) => { $(id).textContent = value; };
const dash = "\u2014";
const number = value => Number.isFinite(value) ? value.toLocaleString() : dash;
const decimal = (value, digits = 3) => Number.isFinite(value) ? value.toFixed(digits) : dash;
const unit = (value, suffix, digits = 3) => Number.isFinite(value) ? decimal(value, digits) + suffix : dash;
const percent = (value, digits = 2) => unit(value == null ? null : value * 100, "%", digits);
const label = value => value ? value.replaceAll("_", " ").replace(/^./, c => c.toUpperCase()) : dash;
const shortHash = hash => hash ? hash.slice(0, 12) : dash;
const views = ["overview", "replay", "logs", "timeline", "run", "models"];
let activeView = "";
let selectedRun = "";
let viewRequest;
let overviewRequest;
let runListRequest;
let scenarios = [];
let modelData;
let timelineOffset = 0;
let timelineProposal = 0;
const timelineLimit = 80;

function element(tag, value, className) {
  const item = document.createElement(tag);
  if (value != null) item.textContent = value;
  if (className) item.className = className;
  return item;
}

function dataList(container, pairs) {
  container.replaceChildren(...pairs.flatMap(([key, value]) => [
    element("dt", key), element("dd", value ?? dash)
  ]));
}

function tableRow(values) {
  const row = element("tr");
  values.forEach(value => row.append(element("td", value ?? dash)));
  return row;
}

function runPath(run, action = "") {
  return "/api/runs/" + encodeURIComponent(run) + action;
}

async function api(path, signal) {
  const response = await fetch(path, { signal, cache: "no-store" });
  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    throw new Error(error.error || "Request failed (" + response.status + ")");
  }
  return response.json();
}

function openView(view) {
  if (location.hash.split("?")[0] !== "#" + view) history.pushState(null, "", "#" + view);
  if (view !== activeView) {
    showView(view);
    $("view-" + view).querySelector("h1").focus({ preventScroll: true });
    window.scrollTo(0, 0);
  }
}

function showView(view) {
  if (view === activeView) return;
  if (activeView === "replay") suspendReplay();
  activeView = view;
  views.forEach(name => { $("view-" + name).hidden = name !== view; });
  document.querySelectorAll("[data-view]").forEach(button => {
    if (button.dataset.view === view) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  });
  loadActiveView();
}

function loadActiveView() {
  viewRequest?.abort();
  viewRequest = new AbortController();
  const signal = viewRequest.signal;
  if (activeView === "replay") openReplay(selectedRun, signal);
  if (activeView === "logs") loadLogs(selectedRun, signal);
  if (activeView === "timeline") loadTimeline(selectedRun, signal);
  if (activeView === "run") loadRunDetails(selectedRun, signal);
}

function selectRun(run, position = null) {
  selectedRun = run;
  $("run-select").value = run;
  timelineOffset = timelineProposal = 0;
  $("timeline-proposal").value = 0;
  resetRecordingViews();
  requestedPosition = position;
  loadActiveView();
}

function replayLink() {
  const [view, query] = location.hash.slice(1).split("?", 2);
  if (view !== "replay" || !query) return null;
  const params = new URLSearchParams(query);
  const run = params.get("run");
  const position = Number(params.get("event"));
  return run && Number.isSafeInteger(position) && position >= 0 ? { run, position } : null;
}

async function loadRuns() {
  runListRequest?.abort();
  runListRequest = new AbortController();
  const { signal } = runListRequest;
  const select = $("run-select");
  try {
    const data = await api("/api/runs", signal);
    if (signal.aborted) return;
    const runs = data.runs || [];
    const recorded = runs.filter(run => run.log_available);
    const summaries = runs.filter(run => !run.log_available);
    setText("metric-recordings", number(recorded.length));
    select.replaceChildren();
    for (const [name, group] of [["Recorded runs", recorded], ["Summary only", summaries]]) {
      if (!group.length) continue;
      const options = element("optgroup");
      options.label = name;
      group.forEach(run => options.append(new Option(run.run_id, run.run_id)));
      select.append(options);
    }
    if (!runs.length) select.append(new Option("No simulation runs found", ""));
    select.disabled = !runs.length;
    const demo = recorded.find(run => run.run_id === "demo_stress");
    const link = selectedRun ? null : replayLink();
    const linkedRun = runs.find(run => run.run_id === link?.run);
    const next = linkedRun?.run_id ?? (runs.some(run => run.run_id === selectedRun) ? selectedRun : (demo || recorded[0] || summaries[0])?.run_id || "");
    selectRun(next, linkedRun ? link.position : null);
    if (link && !linkedRun) setText("replay-copy-status", "The linked run is unavailable on this dashboard.");
  } catch (error) {
    if (signal.aborted) return;
    if (!selectedRun) select.replaceChildren(new Option("Could not load runs", ""));
    setText("connection", "Run list unavailable");
    $("connection").dataset.online = "false";
  }
}

const scenarioMetrics = {
  fork_rate_mean: value => percent(value, 3),
  stale_rate_mean: percent,
  prop_delay_mean: value => unit(value, " s"),
  prop_delay_p95: value => unit(value, " s"),
  goodput_mean: value => unit(value, " TPS", 2),
  node_consistency_mean: percent
};

function renderScenarioChart() {
  const field = $("scenario-metric").value;
  const max = Math.max(0, ...scenarios.map(scenario => scenario[field]));
  $("scenario-chart").replaceChildren(...scenarios.map(scenario => {
    const row = element("div", null, "chart-row");
    const track = element("div", null, "chart-track");
    const bar = element("div", null, "chart-bar");
    const value = scenario[field];
    bar.style.width = (max > 0 && Number.isFinite(value) ? Math.max(0, value / max * 100) : 0) + "%";
    track.setAttribute("aria-hidden", "true");
    track.append(bar);
    row.append(element("span", label(scenario.scenario_name)), track, element("b", scenarioMetrics[field](value)));
    return row;
  }));
}

function renderScenarios(data) {
  scenarios = data.scenarios || [];
  setText("metric-runs", number(scenarios.reduce((count, scenario) => count + scenario.run_count, 0)));
  setText("metric-scenarios", number(scenarios.length) + " scenarios in the matrix");
  $("scenario-rows").replaceChildren(...scenarios.map(s => tableRow([
    label(s.scenario_name), number(s.run_count), percent(s.fork_rate_mean, 3) + " \u00b1 " + percent(s.fork_rate_std), percent(s.stale_rate_mean),
    unit(s.prop_delay_mean, " s"), unit(s.prop_delay_p95, " s"), unit(s.goodput_mean, " TPS", 2), percent(s.node_consistency_mean)
  ])));
  setText("scenario-status", scenarios.length ? "" : "No scenario summary is available.");
  renderScenarioChart();
}

function renderModelScope() {
  const scope = modelData?.scopes?.[$("model-scope").value];
  const models = Object.values(scope?.models || {});
  setText("model-status", scope ? number(scope.test_samples) + " samples / " + percent(scope.prevalence) + " positive" : "No model evaluation is available.");
  $("model-rows").replaceChildren(...models.map(model => tableRow([
    model.model_name, ...["roc_auc", "pr_auc", "f1_score", "precision", "recall", "brier"].map(key => decimal(model[key], 4))
  ])));
  const lr = models.find(model => model.model_name === "LogisticRegression");
  dataList($("model-confusion"), lr ? [
    ["Logistic regression threshold", decimal(lr.threshold, 2)], ["True positives", number(lr.tp)],
    ["False positives", number(lr.fp)], ["False negatives", number(lr.fn)], ["True negatives", number(lr.tn)]
  ] : []);
}

function renderModels(data) {
  modelData = data;
  const scopeSelect = $("model-scope");
  const previous = scopeSelect.value || data.active_scope;
  scopeSelect.replaceChildren(...Object.entries(data.scopes || {}).map(([key, scope]) => new Option(scope.scope_name || label(key), key)));
  if (data.scopes?.[previous]) scopeSelect.value = previous;
  scopeSelect.disabled = !scopeSelect.options.length;
  const production = data.scopes?.production;
  const lr = production?.models?.LogisticRegression;
  setText("metric-auc", decimal(lr?.roc_auc, 4));
  setText("metric-auc-scope", lr ? "Production / " + number(production.test_samples) + " samples" : "Production evaluation unavailable");
  renderModelScope();
}

function renderDrift(data) {
  const active = data.status?.startsWith("Active");
  setText("metric-drift", active ? percent(data.transition_detection_rate, 1) : dash);
  setText("metric-drift-detail", active ? number(data.detected_seeds_count) + " of " + number(data.test_seeds_count) + " seeds detected" : "Drift summary unavailable");
  dataList($("drift-details"), active ? [
    ["Monitored feature", label(data.monitored_feature)], ["Transition proposal", number(data.transition_proposal)],
    ["Transition detection rate", percent(data.transition_detection_rate)], ["Pre-transition false alarms", percent(data.pre_transition_false_positive_rate)],
    ["Post-transition alarms", percent(data.post_transition_ongoing_alarm_rate)], ["Mean detection delay", unit(data.mean_detection_delay_proposals, " proposals", 1)]
  ] : [["Status", data.status || "Unavailable"]]);
}

function loadOverview() {
  overviewRequest?.abort();
  overviewRequest = new AbortController();
  const { signal } = overviewRequest;
  const resources = [
    ["health", data => {
      setText("connection", "API connected");
      $("connection").dataset.online = "true";
      setText("engine-version", data.go_version || "Go simulation engine");
    }, () => {
      setText("connection", "API unavailable");
      $("connection").dataset.online = "false";
    }],
    ["scenarios/summary", renderScenarios, () => setText("scenario-status", "Could not load scenario results.")],
    ["models/evaluation", renderModels, () => setText("model-status", "Could not load model evaluation.")],
    ["drift", renderDrift, () => renderDrift({ status: "Could not load drift results." })],
    ["sensitivity", data => {
      const rows = data.horizons || [];
      $("sensitivity-rows").replaceChildren(...rows.map(point => tableRow([
        point.horizon, percent(point.prevalence), decimal(point.lr_roc_auc, 4), decimal(point.rf_roc_auc, 4)
      ])));
      setText("sensitivity-status", rows.length ? "" : "No sensitivity results are available.");
    }, () => setText("sensitivity-status", "Could not load sensitivity results.")]
  ];
  resources.forEach(([path, render, fail]) => {
    api("/api/" + path, signal).then(data => {
      if (!signal.aborted) render(data);
    }).catch(() => {
      if (!signal.aborted) fail();
    });
  });
}

function renderNodeCards(container, nodes, final = false) {
  const stateNames = { active: final ? "Online" : "Observed", offline: "Offline", unrecorded: "Unrecorded" };
  const sorted = [...nodes].sort((a, b) => a.node_id.localeCompare(b.node_id, undefined, { numeric: true }));
  container.replaceChildren(...sorted.map(node => {
    const card = element("article", null, "node-card");
    const state = node.state || (node.online ? "active" : "offline");
    card.dataset.state = state;
    const header = element("header");
    header.append(element("strong", node.node_id), element("small", stateNames[state] || state));
    const tip = node.tip_hash && !["unknown", "unrecorded"].includes(node.tip_hash);
    const hash = element("p", shortHash(tip && node.tip_hash));
    if (tip) hash.title = node.tip_hash;
    card.append(header, element("p", "Height " + (tip ? number(node.height) : dash)), hash);
    if (final) card.append(element("p", number(node.peers) + " configured peers"));
    return card;
  }));
}

function loadRunDetails(run, signal) {
  $("run-config").replaceChildren();
  $("run-metrics").replaceChildren();
  $("snapshot-cards").replaceChildren();
  setText("run-status", run ? "Loading " + run + "..." : "Select a simulation run.");
  setText("snapshot-status", run ? "Loading node state..." : "Select a simulation run.");
  if (!run) return;
  api(runPath(run), signal).then(data => {
    if (signal.aborted) return;
    const config = data.config || {};
    const m = data.metrics || {};
    setText("run-status", data.run_id + (data.log_available ? " / " + number(data.total_events) + " recorded events" : " / summary only"));
    dataList($("run-config"), [
      ["Scenario", label(data.scenario)], ["Seed", number(data.seed)], ["Proposed blocks", number(data.proposed_blocks)],
      ["Nodes", number(data.node_count)], ["Block interval", unit(config.block_interval_sec, " s")],
      ["Network latency", unit(config.mean_latency_ms, " ms", 1)], ["Propagation delay", unit(config.mean_prop_delay_ms, " ms", 1)],
      ["Transaction arrival rate", unit(config.tx_arrival_rate, " tx/s", 1)]
    ]);
    dataList($("run-metrics"), [
      ["Fork rate", percent(m.fork_rate, 4)], ["Stale rate", percent(m.stale_rate, 4)], ["Fork episodes", number(m.fork_episodes)],
      ["Mean propagation delay", unit(m.prop_time_mean_sec, " s")], ["P95 propagation delay", unit(m.prop_time_p95_sec, " s")],
      ["Offered throughput", unit(m.offered_tps, " TPS", 2)], ["Processed throughput", unit(m.processed_tps, " TPS", 2)],
      ["Finalized goodput", unit(m.goodput, " TPS", 2)], ["Node consistency", percent(m.node_view_consistency)]
    ]);
  }).catch(error => { if (!signal.aborted) setText("run-status", error.message); });

  api(runPath(run, "/nodes"), signal).then(data => {
    if (signal.aborted) return;
    const nodes = data.nodes || [];
    renderNodeCards($("snapshot-cards"), nodes, true);
    setText("snapshot-status", data.log_available ? (nodes.length ? "" : "No node events were recorded.") : "This run includes summary metrics only.");
  }).catch(error => { if (!signal.aborted) setText("snapshot-status", error.message); });
}

async function loadTimeline(run, signal) {
  const offset = timelineOffset;
  const rows = $("timeline-rows");
  rows.replaceChildren();
  $("timeline-prev").disabled = $("timeline-next").disabled = true;
  setText("timeline-status", run ? "Loading consensus events..." : "Select a simulation run.");
  if (!run) return;
  try {
    const query = new URLSearchParams({ limit: timelineLimit + 1, offset, from_proposal: timelineProposal });
    const data = await api(runPath(run, "/timeline?" + query), signal);
    if (signal.aborted) return;
    const events = (data.events || []).slice(0, timelineLimit);
    rows.replaceChildren(...events.map(event => {
      const row = tableRow([unit(event.sim_time, " s"), event.proposal_index || dash, label(event.event_type), event.node_id || dash, event.details]);
      row.lastChild.classList.add("wrap");
      return row;
    }));
    $("timeline-prev").disabled = offset === 0;
    $("timeline-next").disabled = (data.events || []).length <= timelineLimit;
    setText("timeline-status", !data.log_available ? "This run includes summary metrics only." : events.length ? "Events " + (offset + 1) + " to " + (offset + events.length) : "No consensus events match this filter.");
  } catch (error) {
    if (!signal.aborted) setText("timeline-status", error.message);
  }
}

function initDashboard() {
  document.querySelectorAll("[data-view], [data-open]").forEach(button => {
    button.addEventListener("click", () => openView(button.dataset.view || button.dataset.open));
  });
  views.forEach(view => { $("view-" + view).querySelector("h1").tabIndex = -1; });
  const followHash = () => {
    const view = location.hash.slice(1).split("?")[0];
    if (views.includes(view)) showView(view);
    else if (!view || !activeView) showView("overview");
    const link = replayLink();
    if (link && selectedRun && Array.from($("run-select").options).some(option => option.value === link.run)) {
      selectRun(link.run, link.position);
    }
  };
  window.addEventListener("hashchange", followHash);
  $("run-select").addEventListener("change", event => selectRun(event.target.value));
  $("refresh").addEventListener("click", () => { loadOverview(); loadRuns(); });
  $("scenario-metric").addEventListener("change", renderScenarioChart);
  $("model-scope").addEventListener("change", renderModelScope);
  $("timeline-filter").addEventListener("submit", event => {
    event.preventDefault();
    timelineProposal = Number($("timeline-proposal").value);
    timelineOffset = 0;
    loadActiveView();
  });
  $("timeline-prev").addEventListener("click", () => { timelineOffset -= timelineLimit; loadActiveView(); });
  $("timeline-next").addEventListener("click", () => { timelineOffset += timelineLimit; loadActiveView(); });
  initRecordingControls();
  followHash();
  loadOverview();
  loadRuns();
}

document.addEventListener("DOMContentLoaded", initDashboard);
