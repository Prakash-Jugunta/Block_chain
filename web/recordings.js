"use strict";

const replayLimit = 200;
const logLimit = 60;
let logOffset = 0;
let logFilter = {};
let replayPage;
let replayRun = "";
let replayPosition = 0;
let requestedPosition = null;
let replayRequest;
let replayLoading = false;
let replayPlaying = false;
let replayTimer;
let mapEdge;

function resetRecordingViews() {
  suspendReplay();
  resetChainReplay();
  resetReplayTools();
  replayPage = null;
  replayRun = "";
  replayPosition = 0;
  requestedPosition = null;
  $("replay-seek").value = 0;
  logOffset = 0;
}

function eventDetails(entry) {
  if (entry.config_field) return entry.config_field + ": " + (entry.config_old_value ?? dash) + " \u2192 " + (entry.config_new_value ?? dash);
  return [
    entry.from_node && entry.from_node !== entry.node_id ? "From " + entry.from_node : "",
    entry.block_height != null ? "Height " + entry.block_height : "",
    entry.reason,
    entry.block_hash ? shortHash(entry.block_hash) : ""
  ].filter(Boolean).join(" / ") || dash;
}

async function loadLogs(run, signal) {
  const offset = logOffset;
  const rows = $("log-rows");
  rows.replaceChildren();
  $("log-prev").disabled = $("log-next").disabled = true;
  setText("log-status", run ? "Loading recorded events..." : "Select a simulation run.");
  if (!run) return;
  try {
    const query = new URLSearchParams({ limit: logLimit, offset, ...logFilter });
    const page = await api(runPath(run, "/logs?" + query), signal);
    if (signal.aborted) return;
    rows.replaceChildren(...page.events.map(event => {
      const entry = event.entry;
      const row = tableRow([number(event.position), unit(entry.sim_time, " s"), entry.event_type, entry.node_id || dash, eventDetails(entry)]);
      row.lastChild.classList.add("wrap");
      const actions = element("td", null, "actions");
      const json = element("button", "JSON");
      json.type = "button";
      json.setAttribute("aria-label", "View JSON for event " + event.position);
      json.addEventListener("click", () => {
        setText("log-dialog-title", "Event " + number(event.position) + " JSON");
        setText("log-json", JSON.stringify(entry, null, 2));
        $("log-dialog").showModal();
      });
      const replay = element("button", "Replay");
      replay.type = "button";
      replay.setAttribute("aria-label", "Replay event " + event.position);
      replay.addEventListener("click", () => { requestedPosition = event.position; openView("replay"); });
      actions.append(json, replay);
      row.append(actions);
      return row;
    }));
    $("log-prev").disabled = offset === 0;
    $("log-next").disabled = !page.has_more;
    setText("log-status", !page.log_available ? "This run includes summary metrics only. Select a recorded run to browse events." :
      page.events.length ? "Matching rows " + number(offset + 1) + " to " + number(offset + page.events.length) : "No events match these filters.");
  } catch (error) {
    if (!signal.aborted) setText("log-status", error.message);
  }
}

function openReplay(run, viewSignal) {
  loadReplayIndex(run, viewSignal);
  if (replayRun !== run) {
    replayRun = run;
    replayPage = null;
    replayPosition = 0;
    renderReplay();
  }
  if (requestedPosition != null) {
    const target = requestedPosition;
    requestedPosition = null;
    moveReplay(target, viewSignal);
  } else if (replayPage) {
    renderReplay();
  } else if (run) {
    moveReplay(0, viewSignal);
  } else {
    renderReplay();
    setText("replay-status", "Select a simulation run.");
  }
}

function pauseReplay() {
  replayPlaying = false;
  window.clearTimeout(replayTimer);
  setText("replay-play", "Play");
}

function suspendReplay() {
  pauseReplay();
  replayRequest?.abort();
  cancelChainForecast();
  replayLoading = false;
}

function queueReplay() {
  if (!replayPlaying) return;
  window.clearTimeout(replayTimer);
  const target = replayTarget(1);
  if (target == null) {
    pauseReplay();
    updateReplayControls();
    return;
  }
  const current = replayPage.events[replayPosition - replayPage.offset - 1];
  const next = replayPage.events[target - replayPage.offset - 1];
  const delta = next && current ? next.entry.sim_time - current.entry.sim_time : 0.25;
  const delay = $("replay-mode").value === "blocks" ? 1000 : Math.min(900, Math.max(150, delta * 1000));
  replayTimer = window.setTimeout(() => moveReplay(target), delay / Number($("replay-speed").value));
}

function replayTarget(direction) {
  if (!replayPage?.log_available) return null;
  if ($("replay-mode").value === "events") {
    const target = replayPosition + direction;
    const last = replayPage.offset + replayPage.events.length;
    return target >= 0 && (target <= last || replayPage.has_more) ? target : null;
  }
  const { events, prior_block, next_block } = replayPage;
  if (direction > 0) return events.find(event => event.block && event.position > replayPosition)?.position ?? next_block ?? null;
  if (replayPosition === 0) return null;
  return events.findLast(event => event.block && event.position < replayPosition)?.position ?? prior_block ?? 0;
}

async function moveReplay(target, viewSignal = viewRequest.signal) {
  const run = replayRun;
  if (!run || activeView !== "replay" || !Number.isSafeInteger(target) || target < 0 || viewSignal.aborted) return;
  replayRequest?.abort();
  const request = new AbortController();
  replayRequest = request;
  const signal = AbortSignal.any([request.signal, viewSignal]);
  replayLoading = false;
  try {
    const last = replayPage ? replayPage.offset + replayPage.events.length : 0;
    if (!replayPage || (replayPage.offset > 0 && target <= replayPage.offset) || target > last) {
      const offset = Math.floor(Math.max(0, target - 1) / replayLimit) * replayLimit;
      replayLoading = true;
      setText("replay-status", "Loading replay segment...");
      updateReplayControls();
      const segment = start => api(runPath(run, "/replay?" + new URLSearchParams({ limit: replayLimit, offset: start })), signal);
      let page = await segment(offset);
      if (signal.aborted) return;
      if (page.log_available && !page.events.length && page.offset > 0) {
        page = await segment(Math.floor((page.offset - 1) / replayLimit) * replayLimit);
        if (signal.aborted) return;
      }
      replayPage = page;
    }
    replayLoading = false;
    replayPosition = Math.min(target, replayPage.offset + replayPage.events.length);
    if (!replayPage.has_more && replayPosition === replayPage.offset + replayPage.events.length) pauseReplay();
    renderReplay();
    queueReplay();
  } catch (error) {
    if (signal.aborted) return;
    replayLoading = false;
    pauseReplay();
    setText("replay-status", error.message);
    updateReplayControls();
  }
}

function updateReplayControls() {
  const ready = replayPage?.log_available && !replayLoading;
  const end = replayTarget(1) == null;
  const step = $("replay-mode").value === "blocks" ? "block" : "event";
  $("replay-prev").setAttribute("aria-label", "Previous " + step);
  $("replay-next").setAttribute("aria-label", "Next " + step);
  $("replay-prev").disabled = !ready || replayTarget(-1) == null;
  $("replay-next").disabled = !ready || end;
  $("replay-play").disabled = !replayPlaying && (!ready || end);
  $("replay-reset").disabled = !ready || replayPosition === 0;
  $("replay-go").disabled = !replayRun || replayPage?.log_available === false;
  $("replay-seek").disabled = $("replay-go").disabled;
  $("replay-progress").disabled = !ready || replayPage.events.length === 0;
}

function renderReplay() {
  const page = replayPage;
  const applied = page ? page.events.slice(0, replayPosition - page.offset) : [];
  const current = applied.at(-1);
  const blocks = [...(page?.initial_blocks || []), ...applied.filter(event => event.block).map(event => event.block)];
  renderChain(blocks, page?.log_available);
  updateChainForecast(blocks.at(-1), replayRun, viewRequest.signal);
  updateReplayTools(current?.entry.block_hash || blocks.at(-1)?.hash);
  const nodes = new Map((page?.initial_nodes || []).map(node => [node.node_id, node]));
  applied.forEach(event => { if (event.node) nodes.set(event.node.node_id, event.node); });
  renderNetwork([...nodes.values()], current?.entry);
  renderNodeCards($("node-cards"), [...nodes.values()]);
  const eventDetail = $("replay-event");
  eventDetail.replaceChildren();
  if (current) {
    const { entry, proposal } = current;
    const fields = element("dl", null, "data-list");
    dataList(fields, [
      ["Log sequence", number(entry.seq)], ["Proposal", proposal || dash], ["Node", entry.node_id || dash],
      ["Sender", entry.from_node || dash], ["Block height", number(entry.block_height)],
      ["Block hash", shortHash(entry.block_hash)], ["Details", eventDetails(entry)]
    ]);
    eventDetail.append(element("p", entry.event_type, "event-title"), fields);
  } else {
    eventDetail.append(element("p", page?.log_available && page.events.length ? "Replay starts before the first recorded event." : "No event selected.", "status"));
  }
  const progress = $("replay-progress");
  progress.min = replayIndex?.log_available ? 0 : page?.offset ? page.offset + 1 : 0;
  progress.max = replayIndex?.log_available ? replayIndex.total_events : (page?.offset || 0) + (page?.events.length || 0);
  progress.value = replayPosition;
  if (document.activeElement !== $("replay-seek")) $("replay-seek").value = replayPosition;
  setText("replay-position", "Event " + number(replayPosition));
  setText("replay-clock", unit(current?.entry.sim_time ?? page?.start_time ?? 0, " s"));
  setText("replay-status", !page ? "" : !page.log_available ? "This run includes summary metrics only. Select a recorded run to replay nodes." :
    !page.events.length ? "No valid events were found." : "Loaded segment " + number(page.offset + 1) + "\u2013" + number(page.offset + page.events.length) + (page.has_more ? "" : " / Final segment"));
  updateReplayControls();
}

function renderNetwork(nodes, entry) {
  const map = $("network-map");
  const ordered = nodes.sort((a, b) => a.node_id.localeCompare(b.node_id, undefined, { numeric: true }));
  map.replaceChildren();
  mapEdge = null;
  if (!ordered.length) {
    map.append(element("p", "Node activity appears when a recording is loaded.", "status"));
    return;
  }
  if (ordered.length > 24) {
    const total = ordered.length;
    const active = ordered.filter(node => node.node_id === entry?.node_id || node.node_id === entry?.from_node);
    const preview = [...active, ...ordered.filter(node => !active.includes(node))].slice(0, 24);
    ordered.splice(0, ordered.length, ...preview);
    const note = element("small", "Showing 24 of " + number(total) + " nodes. All nodes are listed below.", "map-note");
    map.append(note);
  }
  const canvas = element("canvas");
  canvas.setAttribute("aria-hidden", "true");
  map.append(canvas);
  const positions = new Map();
  ordered.forEach((node, index) => {
    const ring = ordered.length > 12 && index >= 12 ? 1 : 0;
    const count = ring ? ordered.length - 12 : Math.min(ordered.length, 12);
    const angle = ((index - ring * 12) / count) * 2 * Math.PI - Math.PI / 2;
    const x = ordered.length === 1 ? 50 : 50 + Math.cos(angle) * (ring ? 23 : 40);
    const y = ordered.length === 1 ? 50 : 50 + Math.sin(angle) * (ring ? 23 : 39);
    positions.set(node.node_id, [x, y]);
    const marker = element("span", node.node_id, "map-node");
    marker.style.left = x + "%";
    marker.style.top = y + "%";
    marker.dataset.state = node.state;
    marker.title = node.node_id + " / " + node.state;
    if (node.node_id === entry?.node_id || node.node_id === entry?.from_node) marker.classList.add("current");
    map.append(marker);
  });
  if (["BlockGossip", "BlockArrive", "BlockOrphan", "BlockReject"].includes(entry?.event_type) && entry.from_node !== entry.node_id) {
    const from = positions.get(entry.from_node);
    const to = positions.get(entry.node_id);
    if (from && to) mapEdge = { canvas, from, to };
  }
  drawNetworkEdge();
}

function drawNetworkEdge() {
  if (!mapEdge) return;
  const { canvas, from, to } = mapEdge;
  const width = canvas.clientWidth;
  const height = canvas.clientHeight;
  if (!width || !height) return;
  const ratio = window.devicePixelRatio || 1;
  canvas.width = width * ratio;
  canvas.height = height * ratio;
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  ctx.scale(ratio, ratio);
  const start = [from[0] * width / 100, from[1] * height / 100];
  const end = [to[0] * width / 100, to[1] * height / 100];
  const angle = Math.atan2(end[1] - start[1], end[0] - start[0]);
  const arrow = [end[0] - Math.cos(angle) * 22, end[1] - Math.sin(angle) * 22];
  ctx.strokeStyle = "#c5f776";
  ctx.lineWidth = 2;
  ctx.beginPath();
  ctx.moveTo(...start);
  ctx.lineTo(...arrow);
  ctx.moveTo(arrow[0] - Math.cos(angle - 0.55) * 9, arrow[1] - Math.sin(angle - 0.55) * 9);
  ctx.lineTo(...arrow);
  ctx.lineTo(arrow[0] - Math.cos(angle + 0.55) * 9, arrow[1] - Math.sin(angle + 0.55) * 9);
  ctx.stroke();
}

function initRecordingControls() {
  initReplayTools();
  $("log-filters").addEventListener("submit", event => {
    event.preventDefault();
    logFilter = { q: $("log-query").value.trim(), type: $("log-type").value, node: $("log-node").value.trim() };
    logOffset = 0;
    loadActiveView();
  });
  $("log-filters").addEventListener("reset", event => {
    event.preventDefault();
    ["log-query", "log-type", "log-node"].forEach(id => { $(id).value = ""; });
    logFilter = {};
    logOffset = 0;
    loadActiveView();
  });
  $("log-prev").addEventListener("click", () => { logOffset -= logLimit; loadActiveView(); });
  $("log-next").addEventListener("click", () => { logOffset += logLimit; loadActiveView(); });

  const seek = target => { pauseReplay(); moveReplay(target); };
  $("replay-play").addEventListener("click", () => {
    if (replayPlaying) {
      pauseReplay();
    } else {
      replayPlaying = true;
      setText("replay-play", "Pause");
      queueReplay();
    }
  });
  $("replay-speed").addEventListener("change", () => { if (!replayLoading) queueReplay(); });
  $("replay-mode").addEventListener("change", () => {
    updateReplayControls();
    if (!replayLoading) queueReplay();
  });
  $("replay-next").addEventListener("click", () => seek(replayTarget(1)));
  $("replay-prev").addEventListener("click", () => seek(replayTarget(-1)));
  $("replay-reset").addEventListener("click", () => seek(0));
  $("replay-seek-form").addEventListener("submit", event => {
    event.preventDefault();
    seek(Number($("replay-seek").value));
  });
  $("replay-progress").addEventListener("input", event => {
    pauseReplay();
    setText("replay-position", "Event " + number(Number(event.target.value)));
  });
  $("replay-progress").addEventListener("change", event => seek(Number(event.target.value)));
  new ResizeObserver(drawNetworkEdge).observe($("network-map"));
}
