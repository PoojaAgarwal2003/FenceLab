(() => {
  "use strict";
  const get = id => document.getElementById(id);
  const input = get("artifact-file");
  const status = get("artifact-status");
  const error = get("artifact-error");
  const results = get("artifact-results");
  const columns = get("artifact-columns");
  const rows = get("artifact-rows");

  function row(values, heading = false) {
    const tr = document.createElement("tr");
    for (const value of values) {
      const cell = document.createElement(heading ? "th" : "td");
      if (heading) cell.scope = "col";
      cell.textContent = String(value);
      tr.append(cell);
    }
    return tr;
  }

  function render(report) {
    columns.replaceChildren();
    rows.replaceChildren();
    if (report.version === "fencelab/bridge-v1") {
      const s = report.observed;
      get("artifact-heading").textContent = "Four processes. One checked history.";
      get("artifact-summary").textContent = `${s.safe ? "No violations observed" : "INVARIANT VIOLATION"} / ${s.accepted_writes} effects / ${s.stale_writes} stale / ${s.duplicate_writes} duplicate / ${report.history.length} protocol responses match the model.`;
      get("artifact-recovery").textContent = `Authority and store killed and restarted. Recovered epoch ${report.recovered_authority.epoch}; fence ${report.recovered_store.fence}; next reserved token ${report.next_token}. ${report.replay.decisions.length} virtual event decisions; ${report.mode}. ${s.completed ? "Job acknowledged." : "Job not acknowledged in this prefix/run."}`;
      columns.append(row(["Virtual time", "Process", "Operation", "Token", "Response"], true));
      for (const entry of report.history) {
        const r = entry.request;
        const tr = row([`${r.at_ms} ms`, r.to, r.operation, r.token, entry.response.status]);
        if (entry.response.status === "committed") tr.classList.add("artifact-commit");
        rows.append(tr);
      }
    } else {
      get("artifact-heading").textContent = "Fifteen crashes. Recovered effects.";
      get("artifact-summary").textContent = `${report.cases.length} process-kill checkpoints checked / no reused recovered epoch / one logical effect after each retry.`;
      get("artifact-recovery").textContent = "Complete unacknowledged records may survive. Post-sync operations must survive. This is process termination on a local filesystem, not a power-loss guarantee.";
      columns.append(row(["Operation", "Kill point", "Epoch / fence", "Torn bytes", "Retry"], true));
      for (const c of report.cases) {
        rows.append(row([c.operation, c.point, `${c.recovered.epoch} / ${c.recovered.fence}`, c.recovery.truncated_bytes, c.retry.status]));
      }
    }
    get("artifact-empty").hidden = true;
    results.hidden = false;
    status.textContent = "ARTIFACT CONSISTENCY CHECKED";
  }

  input.addEventListener("change", async () => {
    const file = input.files[0];
    if (!file) return;
    input.disabled = true;
    error.hidden = true;
    results.hidden = true;
    status.textContent = "Checking artifact against its invariants...";
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 8000);
    try {
      if (file.size > 65536) throw new Error("Artifact exceeds 64 KiB.");
      const response = await fetch("/api/artifacts/check", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: await file.text(), signal: controller.signal
      });
      const report = await response.json();
      if (!response.ok) throw new Error(report.error || `Artifact check failed (${response.status}).`);
      render(report);
    } catch (failure) {
      error.textContent = failure.name === "AbortError" ? "Artifact check timed out; retry the import." : failure.message;
      error.hidden = false;
      get("artifact-empty").hidden = false;
      rows.replaceChildren();
      status.textContent = "Artifact rejected. No previous result is shown.";
    } finally {
      clearTimeout(timer);
      input.disabled = false;
      input.value = "";
    }
  });
})();
