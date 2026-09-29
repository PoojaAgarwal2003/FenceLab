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
    get("workload-metrics").hidden = report.version !== "fencelab/workload-v1";
    if (report.version === "fencelab/workload-v1") {
      renderWorkload(report);
    } else if (report.version === "fencelab/bridge-v1") {
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

  const ms = ns => `${(ns / 1e6).toFixed(2)} ms`;
  const quantiles = d => `${ms(d.p50_ns)} / ${ms(d.p95_ns)} / ${ms(d.p99_ns)}`;

  function renderWorkload(report) {
    const c = report.counts;
    const config = report.config;
    get("artifact-heading").textContent = "A bounded queue. An honest outcome.";
    get("artifact-summary").textContent = `${c.admitted} admitted / ${c.rejected} rejected / ${c.completed} acknowledged / ${c.exhausted} exhausted. ${c.effects} durable effects; ${c.deduplicated} deduplicated writes across ${c.attempts} attempts. ${c.retries} retries, ${c.lost_acks} lost acknowledgments, ${c.failed_before} failures before write.`;
    const recovery = report.recovery;
    get("artifact-recovery").textContent = `${report.environment}. ${config.jobs} offers, ${config.offer_every_ms} ms spacing, ${config.service_ms} ms declared service per attempt, ${config.max_attempts} attempts maximum. ${recovery.performed
      ? `Clean ledger reopen after attempt ${recovery.after_attempt}: ${recovery.effects} effects / ${recovery.records} records, ${ms(recovery.open_ns)} open/replay, ${ms(recovery.barrier_ns)} new barrier; next token ${recovery.next_token}. Queue stayed in memory; no process was killed.`
      : "No ledger reopen performed in this run."}`;
    get("workload-throughput").textContent = report.acknowledged_jobs_per_second.toFixed(2);
    get("workload-elapsed").textContent = ms(report.elapsed_ns);
    get("workload-capacity").textContent = `${c.high_water} / ${config.capacity}`;
    get("workload-latency").textContent = c.completed ? quantiles(report.completion_latency) : "No acknowledged samples";
    get("workload-wait").textContent = `Initial queue wait p50 / p95 / p99: ${quantiles(report.initial_queue_wait)}. Timed from actual admission; completion latency starts at the offered deadline.`;
    get("workload-fairness").textContent = `${config.policy === "fair" ? "Weighted fair 3:1" : "Strict priority"}: maximum other dispatches while queued = ${report.class_max_wait_dispatches[0]} high / ${report.class_max_wait_dispatches[1]} ordinary. ${report.fair_wait_bound < 0
      ? "No finite starvation bound with continuous higher-priority arrivals."
      : `Conservative per-attempt bound: ${report.fair_wait_bound} other dispatches; not a wall-clock SLA.`}`;
    const outcomes = get("workload-outcomes");
    outcomes.replaceChildren();
    const labels = `${c.completed} acknowledged, ${c.exhausted} exhausted, ${c.rejected} rejected`;
    outcomes.setAttribute("aria-label", labels);
    get("workload-outcome-labels").textContent = labels;
    for (const [name, count] of [["completed", c.completed], ["exhausted", c.exhausted], ["rejected", c.rejected]]) {
      if (!count) continue;
      const segment = document.createElement("span");
      segment.className = `workload-${name}`;
      segment.style.flexGrow = count;
      segment.title = `${name}: ${count}`;
      outcomes.append(segment);
    }
    const dispatches = get("workload-dispatches");
    dispatches.replaceChildren();
    const order = report.order.filter(index => index > 0);
    dispatches.setAttribute("aria-label", `${order.length} actual attempts. High-priority and ordinary class dispatches; per-job details in the table below.`);
    for (const [index, jobIndex] of order.entries()) {
      const job = report.jobs[jobIndex - 1];
      const cell = document.createElement("span");
      cell.className = job.class === 0 ? "workload-high" : "workload-ordinary";
      cell.title = `Dispatch ${index + 1}: ${job.key}, ${job.class === 0 ? "high" : "ordinary"}`;
      dispatches.append(cell);
    }
    columns.append(row(["Job / class", "Outcome", "Attempts", "Max wait (dispatches)", "Offer to terminal"], true));
    for (const job of report.jobs) {
      rows.append(row([`${job.key} / ${job.class === 0 ? "high" : "ordinary"}`, job.status, job.outcomes.length,
        job.max_wait_dispatches, job.status === "rejected" ? "Not admitted" : ms(job.done_ns - job.offered_ns)]));
    }
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
