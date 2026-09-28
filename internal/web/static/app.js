"use strict";

const $ = (id) => document.getElementById(id);
const names = { "lease-only": "Lease only", fenced: "Fencing tokens", "fenced-idempotent": "Fencing + idempotency" };
const actors = { authority: "Authority", "worker-a": "Worker A", "worker-b": "Worker B", store: "Effect store", network: "Network" };
const descriptions = {
  "paused-worker": "Worker A pauses, loses its lease, then returns before Worker B writes.",
  "lost-ack": "A write commits, but its acknowledgment is lost. A retry and a new owner follow.",
  healthy: "No injected fault: one owner, one effect, one successful acknowledgment."
};
const policyExplanations = {
  "lease-only": "A scheduler can reject an obsolete acknowledgment, but it cannot undo a write already accepted by storage. Lease ownership alone does not protect the side effect.",
  fenced: "Storage acknowledges a monotonically increasing fence before each dispatch. Old tokens are rejected, including before the new worker writes. But a new token can still repeat a logical effect after a lost acknowledgment.",
  "fenced-idempotent": "First reject obsolete tokens. Then atomically check a durable, job-scoped effect key and commit only if absent. This protects this model's storage operation, not arbitrary external APIs or all distributed executions."
};
const colors = { info: "#697383", fault: "#a54124", accepted: "#254ab3", rejected: "#246546", deduplicated: "#246546", violation: "#a72c3c" };
let results = [];
let selected = 2;
let cursor = 0;
let timer = null;
let controller = null;
let requestVersion = 0;

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function stopPlayback() {
  if (timer !== null) window.clearInterval(timer);
  timer = null;
  $("play").textContent = "Replay";
}

function displayError(message) {
  $("error").textContent = message;
  $("error").hidden = false;
}

function renderCards() {
  const restoreFocus = $("policies").contains(document.activeElement);
  $("policies").replaceChildren();
  results.forEach((result, index) => {
    const summary = result.summary;
    const button = element("button", `policy-card${index === selected ? " selected" : ""}`);
    button.type = "button";
    button.setAttribute("aria-pressed", String(index === selected));
    button.setAttribute("aria-label", `Inspect ${names[result.config.policy]}`);
    button.append(element("span", "policy-index", `0${index + 1}`));
    button.append(element("span", "policy-name", names[result.config.policy]));
    const metric = element("div");
    metric.append(element("span", "metric", summary.accepted_writes), element("span", "metric-caption", "effects"));
    button.append(metric);
    const bottom = element("div", "policy-bottom");
    bottom.append(element("span", `badge ${summary.safe ? "safe" : "unsafe"}`, summary.safe ? "SAFE IN RUN" : "VIOLATION"));
    bottom.append(element("span", "policy-note", `${summary.stale_writes} stale / ${summary.duplicate_writes} duplicate`));
    button.append(bottom);
    if (index === selected) button.append(element("span", "selection-label", "INSPECTING THIS POLICY"));
    button.addEventListener("click", () => {
      stopPlayback();
      selected = index;
      cursor = result.trace.length - 1;
      render();
    });
    $("policies").append(button);
  });
  if (restoreFocus) $("policies").querySelector('[aria-pressed="true"]').focus({ preventScroll: true });
}

function svgElement(tag, attributes, text) {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, String(value));
  if (text !== undefined) node.textContent = text;
  return node;
}

function renderTimeline(result, svg = $("timeline"), replayCursor = cursor) {
  svg.replaceChildren();
  const lanes = Object.keys(actors);
  const trace = result.trace;
  const lease = result.config ? result.config.lease_ms : result.scenario.lease_ms;
  const end = Math.max(trace.at(-1).at_ms, lease) * 1.08;
  // Equal-time events stack without perturbing virtual time. V2 has more
  // messages per instant, so grow its lane spacing rather than overlap actors.
  const groups = new Map();
  for (const entry of trace) {
    const key = `${entry.actor}:${entry.at_ms}`;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(entry.step);
  }
  const pitch = result.config ? 35 : Math.max(35, ...Array.from(groups.values(), group => group.length * 10 + 14));
  const bottom = 42 + (lanes.length - 1) * pitch + pitch / 2;
  svg.setAttribute("viewBox", `0 0 860 ${bottom + 34}`);
  const x = (at) => 110 + (at / end) * 715;
  const y = (actor) => 42 + lanes.indexOf(actor) * pitch;
  lanes.forEach((actor) => {
    svg.append(svgElement("text", { x: 0, y: y(actor) + 4, class: "lane-label" }, actors[actor]));
    svg.append(svgElement("line", { x1: 107, x2: 838, y1: y(actor), y2: y(actor), class: "lane-line" }));
  });
  const deadlineTime = result.config ? lease : trace.find(entry => entry.action === "ownership_activated")?.at_ms + lease;
  const deadline = x(deadlineTime);
  if (Number.isFinite(deadlineTime)) {
    svg.append(svgElement("line", { x1: deadline, x2: deadline, y1: 24, y2: bottom, class: "deadline" }));
    svg.append(svgElement("text", { x: deadline - 4, y: 14, "text-anchor": "end", class: "axis-label" }, `first lease boundary: ${deadlineTime} ms`));
  }
  for (let tick = 0; tick <= 4; tick++) {
    const at = Math.round((end / 4) * tick);
    svg.append(svgElement("text", { x: x(at), y: bottom + 22, "text-anchor": "middle", class: "axis-label" }, `${at} ms`));
  }
  trace.forEach((entry) => {
    const group = groups.get(`${entry.actor}:${entry.at_ms}`);
    const offset = (group.indexOf(entry.step) - (group.length - 1) / 2) * 10;
    const dot = svgElement("circle", {
      cx: x(entry.at_ms), cy: y(entry.actor) + offset, r: entry.step === replayCursor ? 6.5 : 4.5,
      fill: colors[entry.outcome], opacity: entry.step <= replayCursor ? 1 : 0.25,
      class: `event-dot${entry.step === replayCursor ? " selected" : ""}`
    });
    dot.append(svgElement("title", {}, `${entry.at_ms} ms / ${actors[entry.actor]} / ${entry.action.replaceAll("_", " ")} / token ${entry.token}`));
    svg.append(dot);
  });
  const selectedX = x(trace[replayCursor].at_ms);
  svg.append(svgElement("line", { x1: selectedX, x2: selectedX, y1: 24, y2: bottom, class: "cursor-line" }));
}

function renderTrace(result) {
  const focusedEvent = $("trace").contains(document.activeElement) ? document.activeElement.getAttribute("aria-label") : null;
  $("trace").replaceChildren();
  result.trace.forEach((entry) => {
    const row = element("tr", entry.step === cursor ? "active" : "");
    row.append(element("td", "", `${entry.at_ms} ms`), element("td", "", actors[entry.actor]));
    const eventCell = element("td");
    const button = element("button", "trace-event", entry.action.replaceAll("_", " "));
    button.setAttribute("aria-label", `Inspect event ${entry.step + 1}: ${entry.action.replaceAll("_", " ")}`);
    if (entry.step === cursor) button.setAttribute("aria-current", "step");
    button.addEventListener("click", () => { stopPlayback(); cursor = entry.step; renderReplay(); });
    eventCell.append(button);
    row.append(eventCell, element("td", "", `#${entry.token}`), element("td", `${entry.outcome}-text`, entry.outcome));
    $("trace").append(row);
    if (button.getAttribute("aria-label") === focusedEvent) button.focus({ preventScroll: true });
  });
}

function renderReplay() {
  if (results.length === 0) return;
  const result = results[selected];
  const entry = result.trace[cursor];
  $("replay-title").textContent = names[result.config.policy];
  $("replay-verdict").textContent = result.summary.safe ? "NO VIOLATIONS IN THIS RUN" : `${result.violations.length} INVARIANT VIOLATIONS`;
  $("replay-verdict").className = `badge ${result.summary.safe ? "safe" : "unsafe"}`;
  $("authority-state").textContent = `epoch ${entry.state.epoch}${entry.state.completed ? " / done" : ""}`;
  $("owner-state").textContent = actors[entry.state.owner] || "Unassigned";
  $("store-state").textContent = `fence ${entry.state.storage_fence} / writes ${entry.state.writes}`;
  $("cursor").max = result.trace.length - 1;
  $("cursor").value = cursor;
  $("cursor").disabled = false;
  $("clock").textContent = `t = ${entry.at_ms} ms`;
  $("previous").disabled = cursor === 0;
  $("next").disabled = cursor === result.trace.length - 1;
  $("play").disabled = false;
  $("explanation").replaceChildren(
    element("strong", "", `${cursor + 1}/${result.trace.length} · ${entry.action.replaceAll("_", " ")}. `),
    document.createTextNode(entry.detail)
  );
  $("policy-explanation").textContent = policyExplanations[result.config.policy];
  renderTimeline(result);
  renderTrace(result);
}

function render() {
  renderCards();
  renderReplay();
}

async function runComparison() {
  stopPlayback();
  const version = ++requestVersion;
  if (controller) controller.abort();
  const requestController = new AbortController();
  controller = requestController;
  const query = new URLSearchParams({
    scenario: $("scenario").value, seed: $("seed").value, lease_ms: $("lease").value
  });
  $("error").hidden = true;
  $("run").disabled = true;
  $("download").disabled = true;
  $("status").textContent = "Running the same schedule under three policies...";
  const timeout = window.setTimeout(() => requestController.abort(), 10000);
  try {
    const response = await fetch(`/api/compare?${query}`, { signal: requestController.signal });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || `Server returned ${response.status}`);
    if (version !== requestVersion) return;
    results = body;
    cursor = results[selected].trace.length - 1;
    render();
    $("download").disabled = false;
    $("status").textContent = `REPLAY READY / SEED ${results[0].config.seed} / ${results[0].model}`;
  } catch (error) {
    if (version !== requestVersion) return;
    displayError(error.name === "AbortError" ? "Comparison timed out or was canceled. Run it again." : `Comparison failed: ${error.message}`);
    $("status").textContent = results.length ? "Request failed. Displayed results are from the previous run." : "No results available.";
  } finally {
    window.clearTimeout(timeout);
    if (version === requestVersion) $("run").disabled = false;
  }
}

$("config").addEventListener("submit", (event) => { event.preventDefault(); runComparison(); });
$("scenario").addEventListener("change", () => {
  $("scenario-help").textContent = descriptions[$("scenario").value];
  $("status").textContent = "Settings changed. Run comparison to apply.";
});
for (const input of [$("seed"), $("lease")]) input.addEventListener("input", () => {
  $("status").textContent = "Settings changed. Run comparison to apply.";
});
$("previous").addEventListener("click", () => { stopPlayback(); cursor = Math.max(0, cursor - 1); renderReplay(); });
$("next").addEventListener("click", () => { stopPlayback(); cursor = Math.min(results[selected].trace.length - 1, cursor + 1); renderReplay(); });
$("cursor").addEventListener("input", () => { stopPlayback(); cursor = Number($("cursor").value); renderReplay(); });
$("play").addEventListener("click", () => {
  if (timer !== null) { stopPlayback(); return; }
  if (cursor === results[selected].trace.length - 1) cursor = 0;
  renderReplay();
  $("play").textContent = "Pause";
  timer = window.setInterval(() => {
    cursor = Math.min(results[selected].trace.length - 1, cursor + 1);
    renderReplay();
    if (cursor === results[selected].trace.length - 1) stopPlayback();
  }, 750);
});
$("download").addEventListener("click", () => {
  const blob = new Blob([JSON.stringify(results, null, 2) + "\n"], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = element("a");
  anchor.href = url;
  anchor.download = `fencelab-${results[0].config.scenario}-seed-${results[0].config.seed}.json`;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
});
runComparison();
