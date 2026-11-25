"use strict";

let replayIndex;
let indexedRun = "";
let forkListKey = "";
let adjacentForks = {};
let tracedHash = "";

function resetReplayTools() {
  replayIndex = null;
  indexedRun = "";
  forkListKey = "";
  adjacentForks = {};
  tracedHash = "";
  setText("replay-copy-status", "");
  $("replay-share-url").hidden = true;
  updateReplayTools();
}

async function loadReplayIndex(run, signal) {
  if (!run || (run === indexedRun && replayIndex)) return;
  indexedRun = run;
  setText("fork-index-status", "Finding recorded forks...");
  try {
    const index = await api(runPath(run, "/replay-index"), signal);
    if (signal.aborted) return;
    replayIndex = index;
    if (replayPage) renderReplay();
    else updateReplayTools();
  } catch {
    if (!signal.aborted) setText("fork-index-status", "Could not load the fork index.");
  }
}

function updateReplayTools(hash) {
  tracedHash = hash || "";
  $("trace-block").disabled = !tracedHash;
  $("replay-copy").disabled = !replayRun || !replayPage?.log_available;
  const forks = replayIndex?.forks || [];
  const next = forks.findIndex(fork => fork.position > replayPosition);
  const end = next < 0 ? forks.length : next;
  const onFork = forks[end - 1]?.position === replayPosition;
  adjacentForks = { previous: forks[end - (onFork ? 2 : 1)], next: forks[end] };
  $("fork-prev").disabled = !adjacentForks.previous;
  $("fork-next").disabled = !adjacentForks.next;

  if (!replayIndex) {
    $("fork-markers").replaceChildren();
    if (!indexedRun) setText("fork-index-status", "Select a recorded run to find its forks.");
    return;
  }
  setText("fork-index-status", !replayIndex.log_available ? "This run includes summary metrics only." :
    !forks.length ? "No competing branches were recorded in this run." :
      number(forks.length) + " competing proposals across " + number(replayIndex.total_events) + " recorded events.");
  const key = end + ":" + onFork;
  if (key === forkListKey) return;
  forkListKey = key;

  const start = Math.min(Math.max(0, end - 2), Math.max(0, forks.length - 5));
  $("fork-markers").replaceChildren(...forks.slice(start, start + 5).map(fork => {
    const marker = element("button", null, "fork-marker");
    marker.type = "button";
    if (fork.position === replayPosition) marker.setAttribute("aria-current", "step");
    marker.append(element("strong", "Block " + number(fork.height)),
      element("span", "Event " + number(fork.position) + " / " + fork.node),
      element("small", unit(fork.sim_time, " s", 2)));
    marker.addEventListener("click", () => jumpToReplay(fork.position));
    return marker;
  }));
}

function jumpToReplay(position) {
  if (activeView !== "replay" || !replayRun) return;
  pauseReplay();
  moveReplay(position);
}

async function copyReplayPosition() {
  const url = new URL(location.href);
  url.hash = "replay?" + new URLSearchParams({ run: replayRun, event: replayPosition });
  const field = $("replay-share-url");
  try {
    await navigator.clipboard.writeText(url.href);
    field.hidden = true;
    setText("replay-copy-status", "Replay position link copied.");
  } catch {
    field.value = url.href;
    field.hidden = false;
    field.focus();
    field.select();
    setText("replay-copy-status", "Copy the selected link.");
  }
}

function traceReplayBlock() {
  if (!tracedHash) return;
  $("log-query").value = tracedHash;
  $("log-type").value = "";
  $("log-node").value = "";
  logFilter = { q: tracedHash };
  logOffset = 0;
  openView("logs");
}

function initReplayTools() {
  $("fork-prev").addEventListener("click", () => jumpToReplay(adjacentForks.previous.position));
  $("fork-next").addEventListener("click", () => jumpToReplay(adjacentForks.next.position));
  $("replay-copy").addEventListener("click", copyReplayPosition);
  $("trace-block").addEventListener("click", traceReplayBlock);
  document.addEventListener("keydown", event => {
    if (activeView !== "replay" || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey ||
        event.target.closest("input, select, textarea, button, a, dialog, [contenteditable], .chain-scroll")) return;
    const key = { " ": "replay-play", ArrowLeft: "replay-prev", ArrowRight: "replay-next" }[event.key];
    if (!key || $(key).disabled) return;
    event.preventDefault();
    $(key).click();
  });
}
