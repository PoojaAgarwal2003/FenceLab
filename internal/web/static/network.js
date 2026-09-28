"use strict";

(() => {
  const byId = id => document.getElementById(id);
  let examples = null;
  let result = null;
  let replay = null;
  let reportText = "";
  let busy = false;

  function invalidate() {
    replay = null;
    byId("network-export").disabled = true;
    byId("network-status").textContent = "Settings changed. Any displayed transport results are from the previous run.";
  }

  function render() {
    if (!result) return;
    const cursor = Number(byId("network-cursor").value);
    const entry = result.trace[cursor];
    renderTimeline(result, byId("network-timeline"), cursor);
    byId("network-clock").textContent = `t = ${entry.at_ms} ms`;
    byId("network-explanation").textContent = `${cursor + 1}/${result.trace.length}: ${entry.action.replaceAll("_", " ")}. ${entry.detail} Attempt token ${entry.token}; active epoch ${entry.state.epoch}; storage fence ${entry.state.storage_fence}; writes ${entry.state.writes}.`;
    const journal = byId("network-trace");
    const focused = journal.contains(document.activeElement) ? document.activeElement.getAttribute("aria-label") : null;
    journal.replaceChildren();
    for (const event of result.trace) {
      const row = document.createElement("tr");
      if (event.step === cursor) row.className = "active";
      for (const text of [`${event.at_ms} ms`, actors[event.actor]]) {
        const cell = document.createElement("td");
        cell.textContent = text;
        row.append(cell);
      }
      const cell = document.createElement("td");
      const button = document.createElement("button");
      button.className = "trace-event";
      button.textContent = event.action.replaceAll("_", " ");
      button.setAttribute("aria-label", `Inspect transport event ${event.step + 1}`);
      if (event.step === cursor) button.setAttribute("aria-current", "step");
      button.addEventListener("click", () => { byId("network-cursor").value = event.step; render(); });
      cell.append(button);
      row.append(cell);
      const token = document.createElement("td");
      token.textContent = `#${event.token}`;
      row.append(token);
      journal.append(row);
      if (button.getAttribute("aria-label") === focused) button.focus({ preventScroll: true });
    }
  }

  async function execute(operation, body) {
    if (busy) return;
    busy = true;
    replay = null;
    for (const id of ["network-run", "network-search", "network-example", "network-json", "max-states", "max-depth", "replay-file", "network-export"]) byId(id).disabled = true;
    byId("network-error").hidden = true;
    byId("network-status").textContent = operation === "search" ? "Exploring bounded delivery interleavings..." : "Executing virtual message deliveries...";
    const abort = new AbortController();
    const timeout = setTimeout(() => abort.abort(), 8000);
    try {
      const response = await fetch(`/api/v2/${operation}`, {
        method: "POST", headers: { "Content-Type": "application/json" }, body, signal: abort.signal
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
      if (operation === "search") {
        result = data.counterexample;
        replay = data.witness;
        reportText = `${data.status} / ${data.states} stored states / ${data.transitions} transitions / depth ${data.depth_reached} / ${data.merged} merges / ${data.terminal_states} terminal states / ${data.incomplete_jobs} incomplete jobs. Bounds: ${data.bounds.max_states} states, ${data.bounds.max_depth} decisions. ${data.minimality} ${data.scope}`;
      } else {
        result = data;
        replay = data.replay;
        reportText = `${data.scenario.name} (${data.scenario.protocol}, ${data.scenario.policy}). ${data.summary.safe ? "No violations observed" : "INVARIANT VIOLATION"} / ${data.summary.accepted_writes} effects / ${data.summary.stale_writes} stale / ${data.summary.duplicate_writes} duplicate / ${data.summary.completed ? "job acknowledged" : "job incomplete"} / ${data.halt_reason} / ${data.pending.length} pending events.`;
        if (operation === "replay") {
          byId("network-json").value = JSON.stringify(data.scenario, null, 2);
          byId("network-example").value = "custom";
        }
      }
      byId("search-report").textContent = reportText;
      byId("network-status").textContent = `V2 ${operation.toUpperCase()} READY`;
      byId("network-export").disabled = !replay;
      byId("network-cursor").disabled = !result;
      if (result) {
        byId("network-cursor").max = result.trace.length - 1;
        byId("network-cursor").value = result.trace.length - 1;
        render();
      } else {
        byId("network-timeline").replaceChildren();
        byId("network-trace").replaceChildren();
        byId("network-clock").textContent = "No trace";
        byId("network-explanation").textContent = "No counterexample trace. A bound being reached is inconclusive; exhausted applies only to this fixed scenario's enabled interleavings.";
      }
    } catch (error) {
      byId("network-error").textContent = error.name === "AbortError" ? "Transport request timed out or was canceled." : `Transport request failed: ${error.message}`;
      byId("network-error").hidden = false;
      byId("network-status").textContent = "Request failed. Any displayed transport results are from the previous run.";
    } finally {
      clearTimeout(timeout);
      busy = false;
      for (const id of ["network-run", "network-search", "network-example", "network-json", "max-states", "max-depth", "replay-file"]) byId(id).disabled = false;
    }
  }

  byId("network-example").addEventListener("change", () => {
    if (!examples) return;
    byId("network-json").value = JSON.stringify(examples[byId("network-example").value], null, 2);
    invalidate();
  });
  for (const id of ["network-json", "max-states", "max-depth"]) byId(id).addEventListener("input", invalidate);
  byId("network-json").addEventListener("input", () => { byId("network-example").value = "custom"; });
  byId("network-run").addEventListener("click", () => execute("run", byId("network-json").value));
  byId("network-search").addEventListener("click", () => {
    if (!byId("max-states").reportValidity() || !byId("max-depth").reportValidity()) return;
    const bounds = { max_states: Number(byId("max-states").value), max_depth: Number(byId("max-depth").value) };
    execute("search", `{"scenario":${byId("network-json").value},"bounds":${JSON.stringify(bounds)}}`);
  });
  byId("network-cursor").addEventListener("input", render);
  byId("replay-file").addEventListener("change", async () => {
    const file = byId("replay-file").files[0];
    if (!file) return;
    try {
      if (file.size > 65536) throw new Error("Replay file exceeds 64 KiB.");
      await execute("replay", await file.text());
    } catch (error) {
      invalidate();
      byId("network-error").textContent = `Cannot read replay: ${error.message}`;
      byId("network-error").hidden = false;
    } finally {
      byId("replay-file").value = "";
    }
  });
  byId("network-export").addEventListener("click", () => {
    if (!replay) return;
    const blob = new Blob([JSON.stringify(replay, null, 2) + "\n"], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "fencelab-v2-replay.json";
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  });
  fetch("/api/v2/examples", { signal: AbortSignal.timeout(8000) }).then(async response => {
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    examples = await response.json();
    byId("network-example").value = "barrier";
    byId("network-json").value = JSON.stringify(examples.barrier, null, 2);
    byId("network-status").textContent = "V2 scenarios ready. Run or explore; no real network is used.";
    byId("network-run").disabled = false;
    byId("network-search").disabled = false;
    byId("network-example").disabled = false;
    byId("network-json").disabled = false;
  }).catch(error => {
    byId("network-status").textContent = "Scenario loading failed. Reload to retry.";
    byId("network-error").textContent = error.message;
    byId("network-error").hidden = false;
  });
})();
