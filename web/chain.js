"use strict";

const chainWindow = 14;
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
let chainKey = "";
let lastChainBlock = 0;
let forecastKey = "";
let forecastCache = new Map();
let forecastRequest;

function resetChainReplay() {
  cancelChainForecast();
  forecastCache = new Map();
  forecastKey = "";
  chainKey = "";
  lastChainBlock = 0;
}

function cancelChainForecast() {
  forecastRequest?.abort();
}

function renderChain(blocks, canReplay) {
  const latest = blocks.at(-1);
  const key = (canReplay ? "ready" : "unavailable") + ":" + (latest?.position || 0);
  if (key === chainKey) return;
  chainKey = key;
  const animate = latest && latest.position > lastChainBlock;
  lastChainBlock = latest?.position || 0;

  const badge = $("chain-state");
  badge.dataset.fork = Boolean(latest?.fork);
  badge.textContent = latest ? (latest.fork ? "Fork at block " : "Block ") + number(latest.height) : "Ready";
  const track = $("chain-track");
  track.replaceChildren();
  if (!canReplay) {
    track.append(element("p", "Select a recorded run to build the chain.", "status"));
    track.style.width = "";
    track.style.height = "";
    return;
  }

  const visible = blocks.slice(-chainWindow);
  if (!visible.length || visible[0].height === 1) {
    const genesis = { hash: visible[0]?.parent || "genesis", height: 0, node: "Start of chain" };
    visible.unshift(genesis);
  }
  const heights = [...new Set(visible.map(block => block.height))].sort((a, b) => a - b);
  const cols = new Map(heights.map((height, index) => [height, index]));
  const lanes = new Map([[0, 0]]);
  const points = new Map();
  const width = heights.length * 140;
  let laneCount = 1;
  visible.forEach(block => {
    const branch = block.branch || 0;
    if (!lanes.has(branch)) lanes.set(branch, laneCount++);
    points.set(block.hash, [18 + cols.get(block.height) * 140, 21 + lanes.get(branch) * 84]);
  });
  const height = Math.max(210, 42 + laneCount * 84);
  track.style.width = width + "px";
  track.style.height = height + "px";
  const canvas = element("canvas");
  canvas.setAttribute("aria-hidden", "true");
  const ratio = window.devicePixelRatio || 1;
  canvas.width = width * ratio;
  canvas.height = height * ratio;
  canvas.style.width = width + "px";
  canvas.style.height = height + "px";
  track.append(canvas);
  const ctx = canvas.getContext("2d");
  ctx?.scale(ratio, ratio);
  visible.forEach(block => {
    const [x, y] = points.get(block.hash);
    const parent = points.get(block.parent);
    if (parent && ctx) {
      const fromX = parent[0] + 104;
      const fromY = parent[1] + 35;
      ctx.strokeStyle = block.branch ? "#ef7c78" : "#86b981";
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(fromX, fromY);
      const midpoint = (fromX + x) / 2;
      ctx.bezierCurveTo(midpoint, fromY, midpoint, y + 35, x, y + 35);
      ctx.stroke();
    }
    const card = element(block.height ? "button" : "div", null, "chain-block");
    const current = block === latest;
    card.dataset.branch = Boolean(block.branch);
    card.dataset.current = current;
    card.dataset.fork = Boolean(block.fork);
    if (current && animate) card.classList.add("created");
    card.style.left = x + "px";
    card.style.top = y + "px";
    card.title = block.hash;
    if (block.height) {
      card.type = "button";
      card.setAttribute("aria-label", "Replay block " + block.height + " proposed by " + block.node);
      card.addEventListener("click", () => jumpToReplay(block.position));
    }
    card.append(element("strong", block.height ? "Block " + number(block.height) : "Genesis"),
      element("span", block.node || dash),
      element("small", block.fork ? "Fork created" : block.height ? shortHash(block.hash) : "Height 0"));
    track.append(card);
  });

  const scroller = $("chain-scroll");
  if (latest) {
    const [x, y] = points.get(latest.hash);
    scroller.scrollTo({
      left: Math.max(0, x + 126 - scroller.clientWidth),
      top: Math.max(0, y + 94 - scroller.clientHeight),
      behavior: animate && !reducedMotion.matches ? "smooth" : "instant"
    });
  }
}

function updateChainForecast(block, run, signal) {
  const key = block ? run + ":" + block.position : "";
  const container = $("chain-forecast");
  if (!block) {
    forecastRequest?.abort();
    forecastKey = "";
    container.replaceChildren(element("p", "Play a recorded run to see forecasts.", "status"));
    return;
  }
  if (forecastKey === key && forecastRequest && !forecastRequest.signal.aborted) return;
  forecastKey = key;
  forecastRequest?.abort();
  forecastRequest = new AbortController();
  const request = forecastRequest;
  const requestSignal = AbortSignal.any([signal, request.signal]);
  if (forecastCache.has(key)) {
    renderForecast(block, forecastCache.get(key));
    return;
  }
  container.replaceChildren(element("p", "Calculating forecast for block " + number(block.height) + "...", "status"));
  api(runPath(run, "/forecast?position=" + block.position), requestSignal).then(result => {
    if (requestSignal.aborted) return;
    if (forecastCache.size >= 96) forecastCache.delete(forecastCache.keys().next().value);
    forecastCache.set(key, result);
    renderForecast(block, result);
  }).catch(() => {
    if (!requestSignal.aborted) {
      container.replaceChildren(element("p", "Could not load the model forecast.", "status"));
      request.abort();
    }
  });
}

function renderForecast(block, result) {
  const container = $("chain-forecast");
  container.replaceChildren();
  if (result.message) container.append(element("p", result.message, "status"));
  const models = [result, result.forest].filter(model => model?.scores.length);
  if (models.length) container.append(element("p", "Forecast window: next " + result.horizon + " block heights.", "forecast-window"));
  if (block.fork && !models.length && !result.message) {
    container.append(element("p", "No earlier model forecast covers this fork.", "forecast-outcome"));
  }
  models.forEach(model => container.append(renderForecastModel(block, model, result.horizon)));
}

function renderForecastModel(block, model, horizon) {
  const card = element("div", null, "forecast-model");
  card.append(element("h3", model.model));
  const score = model.scores.find(item => item.height === block.height);
  if (!block.fork && score) {
    const value = element("div", null, "forecast-score");
    value.dataset.high = score.high_risk;
    value.append(element("strong", percent(score.probability, 1)),
      element("span", score.high_risk ? "Elevated risk" : "Below threshold"));
    const meter = element("div", null, "forecast-meter");
    meter.dataset.high = score.high_risk;
    meter.setAttribute("aria-hidden", "true");
    const fill = element("i");
    fill.style.width = percent(score.probability, 2);
    const threshold = element("b");
    threshold.style.left = percent(model.threshold, 2);
    meter.append(fill, threshold);
    card.append(value, meter);
  }
  if (block.fork) {
    const prior = model.scores.filter(item => item.position < block.position &&
      item.height < block.height && item.height + horizon >= block.height);
    const warnings = prior.filter(item => item.high_risk);
    const verdict = warnings.length ? "Model flagged elevated risk before this fork." :
      prior.length ? "Earlier forecasts stayed below the warning threshold." :
        "No earlier model forecast covers this fork.";
    card.append(element("p", verdict, "forecast-outcome"));
    if (prior.length) card.append(forecastHistory(prior));
  } else {
    const prior = model.scores.filter(item => item.height !== block.height);
    if (prior.length) card.append(forecastHistory(prior));
  }
  card.append(element("p", "Warning threshold " + percent(model.threshold, 0), "forecast-window"));
  return card;
}

function forecastHistory(scores) {
  const history = element("div", null, "forecast-history");
  history.setAttribute("aria-label", "Earlier forecast scores");
  history.append(...scores.map(item => {
    const score = element("span", "#" + number(item.height) + " " + percent(item.probability, 1));
    score.dataset.high = item.high_risk;
    return score;
  }));
  return history;
}
