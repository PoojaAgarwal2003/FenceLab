# FenceLab evidence

Recorded on 2026-09-25 from application commit `dafd5b3`.
The later presentation commit adds this record, images, and CI, not a different
simulation model.

## Reproduced outcomes

```sh
go run ./cmd/fencelab check -start-seed 0 -seeds 1000 -lease-ms 100
```

The [raw report](evidence/seed-sweep.json) contains:

- 9,000 runs: 1,000 seeds x three scenario templates x three policies.
- 3,000 expected unsafe runs.
- Zero protected-policy failures and zero unexpected outcomes.
- A replayable unsafe witness, including its configuration and exact
  `effect_committed` trace steps.

Replay its first witness:

```sh
go run ./cmd/fencelab run -scenario paused-worker -policy lease-only -seed 0 -lease-ms 100
```

This is controlled regression exploration, **not** randomized production traffic,
exhaustive state-space coverage, a failure-rate estimate, or a correctness proof.
The templates deliberately reach the lease-handoff window.

## Local checks

Environment: Windows amd64, Go 1.27.1, Node.js 22.17.0,
Playwright 1.63.0. No cloud or external data was used.

The first milestone exercises:

- Exact expected results for all nine policy/scenario pairs.
- 1,000 additional seeds with varied lease lengths and an independent
  committed-effect oracle.
- Byte-identical deterministic replay, int64 seed extremes, invalid inputs,
  stable same-time event ordering, and no shared mutable run state.
- CLI help/errors, output-write failures, seed-range overflow and cancellation.
- Real loopback lifecycle, invalid listeners, query/Host/origin checks,
  deterministic admission saturation, explicit HTTP 429, and recovery.
- 14 real Chromium cases: seven checks each on desktop and mobile, including
  replay controls, exported JSON equality, both failures, healthy control,
  bounded input, visible errors, retry recovery, and page-width checks.
- A clean `npm ci` followed by the browser suite, and a standalone binary
  launched outside the repository that served the embedded dashboard, assets,
  and real comparison API without source files or `node_modules`.

See the [CI workflow](../.github/workflows/ci.yml) for fresh Windows/Linux checks.
Linux native/race results must come from that workflow, not be inferred from a
Windows run.

## Original milestone screenshots

The [desktop](images/laboratory-desktop.png) and
[mobile](images/laboratory-mobile.png) images come directly from the first browser
test against the real Go server. They show model results for the paused-worker
scenario at seed `7` and virtual lease `100 ms`; no metric was painted over.

These original images are preserved rather than overwritten by the redesign.

## Workbench redesign - 2026-09-28

The [current desktop](images/workbench-desktop.png) and
[current mobile](images/workbench-mobile.png) captures show the new light,
execution-first investigation workbench. Conditions are arranged horizontally,
policy outcomes form a vertical ledger, and the trace lives in an event journal.
The simulation model and recorded 9,000-run report above are unchanged.

The redesigned interface passed 20 Chromium cases (10 each in the desktop and
mobile projects). In addition to the original seven checks per project, these
cover the light theme and new layout geometry, keyboard focus retention and
current-event semantics, and widths of 320, 768, 1024, 1440, and 1920 CSS pixels.
Go tests, vet, and the rebuilt embedded-UI binary also passed locally on Windows.
These local results do not claim a new GitHub Actions run.

To refresh the current screenshots, run `npm run test:e2e`, review the two
`laboratory.png` files under `web-test-results/`, and copy the intended captures
to `docs/images/workbench-desktop.png` and `docs/images/workbench-mobile.png`.
Keep the original milestone captures. Do not present virtual event times as
measured application latency.

## Milestones 2-3 - 2026-09-28

Recorded from the local v2 implementation using the rebuilt Windows binary;
these results are not a claim that unpushed changes passed GitHub Actions.
The four [scenario files](../examples/eager.json) are checked against the
browser's built-in examples by a Go test.

| Scenario | Bounds | Result | Stored states | Transitions | Depth |
|---|---|---|---:|---:|---:|
| Eager dispatch, delayed replacement fence | 2000 states / 80 decisions | Counterexample | 12 | 13 | 9 |
| Acknowledged barrier, delayed replacement fence | 2000 states / 80 decisions | Exhausted fixed scenario | 19 | 18 | 18 |

The eager case has a **9-decision shortest violating prefix**. Its commit
uses token 1 while active epoch is 2 and storage fence is still 1.
The barrier run reaches one terminal state with no incomplete job and no
violation. These tiny state counts describe the checked-in scenarios, not the
scale of a real cluster or a general correctness proof.

Reproduce the reports:

```sh
go run ./cmd/fencelab search -file examples/eager.json -max-states 2000 -max-depth 80
go run ./cmd/fencelab search -file examples/barrier.json -max-states 2000 -max-depth 80
go run ./cmd/fencelab replay -file docs/evidence/v2-counterexample.json
```

Raw artifacts:
[unsafe search](evidence/v2-eager-search.json),
[barrier search](evidence/v2-barrier-search.json), and
[replay witness](evidence/v2-counterexample.json).
The witness reproduces the report's counterexample result exactly.

Local checks additionally cover:

- Byte-identical v1 output from the pre-milestone and rebuilt binaries for
  12 comparisons (36 policy runs), including int64 seed extremes.
- The existing 9,000-run v1 sweep still has zero protected failures or
  unexpected outcomes.
- Independent committed-effect counts; all fault types; monotonic fences;
  worker clock skew that does not affect authority; stalled barrier outcomes.
- BFS against unmerged enumeration; a same-time race with both safe and unsafe
  orders; positive state merging; shortest depth, explicit cutoffs, cancellation.
- Strict JSON, checked-in examples, CLI witness files that cannot overwrite
  existing files, exact API replay, loopback/origin and body/admission limits.
- 32 Chromium cases: 16 each desktop/mobile, including the 20 workbench cases
  and 12 new transport/search/export/import/error cases.
- Go tests, vet, and build; `internal/model` statement coverage was 90.7%.

The [transport desktop](images/transport-desktop.png) and
[transport mobile](images/transport-mobile.png) captures show the actual unsafe
write selected in the new laboratory. Original v1 and redesign screenshots
remain preserved. Refresh these using the `transport-laboratory.png` output
from `tests/network.spec.mjs`, not by editing displayed results into an image.

See [the v2 semantic boundary and search limits](transport-and-search.md)
before interpreting any of these outcomes.

## Milestones 4-5 - 2026-09-28

The following reports came from the rebuilt Windows binary, not from painted
dashboard values. No new Linux runtime or GitHub Actions result is claimed for
these local commits.

| Actual experiment | Recorded outcome | Artifact |
|---|---|---|
| Reserve/fence/write crossed with five crash checkpoints | 15 forced child-process kills; all recovery invariants hold; retry leaves one effect | [Crash matrix](evidence/durability.json) |
| Eager exact counterexample prefix | Four actor processes; 9 decisions; 12 matching protocol responses; one stale effect | [Eager history](evidence/process-eager.json) |
| Barrier, seed 7 | One effect; zero stale/duplicate effects; two deduplications; job acknowledged | [Barrier history](evidence/process-barrier.json) |
| Lost result and duplicate write, seed 11 | One effect; zero stale/duplicate effects; three deduplications; job acknowledged | [Lost-result history](evidence/process-lost-result.json) |

Every bridge run forcibly kills and restarts the authority and store after
its selected history, compares their recovered ledger state, and reserves a
fresh token. The eager witness ends before job acknowledgment, intentionally.
Safety and completion remain separate.

Reproduce with fresh directories:

```sh
go run ./cmd/fencelab durability -dir crash-lab
go run ./cmd/fencelab bridge -replay docs/evidence/v2-counterexample.json -dir bridge-eager
go run ./cmd/fencelab bridge -file examples/barrier.json -seed 7 -dir bridge-barrier
go run ./cmd/fencelab bridge -file examples/lost-result.json -seed 11 -dir bridge-retry
```

Local validation covers byte-by-byte incomplete tails and corruption, exclusive
ownership, concurrent callers, poisoned writers, actual process death, restarted
epoch activation guards, invalid protocol frames, policy controls, partition
healing, and extreme worker clock annotations. The old eager/barrier v2 search
reports still compare identically after introducing the transport interface.

The complete Go tests, vet, and build passed. All **40 Chromium cases** passed:
20 each desktop/mobile, including eight new artifact inspection, tampering,
size-limit, service-error, and retry cases. Artifact API tests recheck all four
curated reports and reject modified claims.

A Linux amd64 cross-build passed (compilation only, not native runtime or race
evidence). An isolated copy of the Windows executable reproduced the exact
four-process counterexample and served the embedded artifact checker without
Go, npm, or a source checkout in its working directory. Its owned server and
child processes were stopped after the check.

The [process desktop](images/process-desktop.png),
[process mobile](images/process-mobile.png),
[recovery desktop](images/recovery-desktop.png), and
[recovery mobile](images/recovery-mobile.png) captures are generated by
`tests/artifacts.spec.mjs`. All earlier images remain preserved.

This is evidence of local **process-crash recovery and controlled protocol
execution**, not power-loss durability, autonomous networking, external
exactly-once effects, or a throughput benchmark. See the
[WAL contract](durable-recovery.md) and [bridge scope](process-bridge.md).

## Milestone 6 - 2026-09-29

Recorded from the Windows executable built at `8783976`, using the four
checked-in workload configs. Visible environment: Windows amd64, Go 1.27.1,
Intel Xeon Platinum 8370C at 2.80 GHz (8 exposed cores / 16 logical processors).
WALs were created in new local directories under the working checkout, in a
OneDrive-managed folder. Storage/sync activity and host contention were not
isolated. No cloud API or remote service was used by FenceLab.

**One actual run per configuration, in the table's order; no warmup or repeated-run
confidence interval.** These are short, machine-specific demonstrations, not
comparative performance claims. Completion percentiles describe acknowledged jobs
inside one batch; they are not percentiles across repeated benchmarks.

| Declared workload | Admitted / rejected | Acknowledged / exhausted | Durable effects | Attempts / deduplications | Artifact |
|---|---:|---:|---:|---:|---|
| Fair no-fault control | 64 / 0 | 64 / 0 | 64 | 64 / 0 | [Balanced](evidence/workload-balanced.json) |
| Overload + first lost acknowledgment | 16 / 112 | 16 / 0 | 16 | 20 / 4 | [Overload](evidence/workload-overload.json) |
| Always-lost acknowledgment retry storm | 64 / 0 | 48 / 16 | 64 | 96 / 32 | [Retry storm](evidence/workload-retry-storm.json) |
| Strict-priority no-fault control | 64 / 0 | 64 / 0 | 64 | 64 / 0 | [Priority](evidence/workload-priority.json) |

| Workload | Batch elapsed (ms) | Acknowledged jobs/s | Completion p50 / p95 / p99 (ms) | Ledger open/replay (ms) | New epoch + fence (ms) |
|---|---:|---:|---:|---:|---:|
| Balanced | 125.654 | 509.337 | 70.623 / 120.270 / 125.654 | 11.393 | 1.606 |
| Overload | 67.424 | 237.306 | 41.998 / 67.424 / 67.424 | 10.387 | 1.120 |
| Retry storm | 155.484 | 308.713 | 59.971 / 113.181 / 117.078 | 2.031 | 1.091 |
| Priority | 130.849 | 489.115 | 75.627 / 125.984 / 130.849 | 14.379 | 1.076 |

The first ordinary job in the no-fault batch waits **3 other dispatches with fair
selection versus 48 with strict priority**. Both finite batches drain. The
continuous-high-arrival unit control demonstrates why that is not a starvation
guarantee for strict priority. Fair selection's conservative `4C - 1` bound is
per queued attempt, not an end-to-end or wall-clock guarantee.

The retry storm's 16 exhausted jobs already have durable effects; reporting 64
completed jobs would be wrong. The overload's outstanding high water is exactly
16 including the active job and retry slots, not 128 silently buffered jobs.

Reproduce with new directories:

```sh
go run ./cmd/fencelab workload -file examples/workload-balanced.json -dir workload-balanced
go run ./cmd/fencelab workload -file examples/workload-overload.json -dir workload-overload
go run ./cmd/fencelab workload -file examples/workload-retry-storm.json -dir workload-retry-storm
go run ./cmd/fencelab workload -file examples/workload-priority.json -dir workload-priority
```

Validation on Windows passed the full Go tests, vet, binary build, and all
**50 Chromium cases** (25 each desktop/mobile). New checks cover capacity during
execution/retry, FIFO fairness, strict-priority starvation, attempt exhaustion,
actual WAL deduplication and reopen, paced offers, cancellation, forged metrics,
changed selection journals, and curated report/config agreement. The maximum
128-job/five-attempt generated report remained below the 64 KiB import limit.
Browser tests now serialize success requests against the intentionally
single-slot model/artifact executor; overload/error behavior is tested explicitly.

The rebuilt executable's v1 9,000-run sweep and both v2 eager/barrier search
reports deep-compare identically with the preserved artifacts. A Linux amd64
cross-build passed; **no new native Linux or race result is claimed** before CI.
An isolated copy of the Windows executable ran the retry-storm workload and
served the embedded dashboard/artifact API without a source checkout or runtime
Go/Node dependency in its working directory. Its owned server was stopped.

The [retry-storm desktop](images/workload-desktop.png) and
[mobile](images/workload-mobile.png), plus [overload desktop](images/workload-overload-desktop.png)
and [mobile](images/workload-overload-mobile.png), are direct browser captures of
these curated reports from `tests/workloads.spec.mjs`. Earlier images are unchanged.

These measurements include a **clean ledger close/reopen**, while the in-memory
queue survives. They do not measure process-crash queue recovery, power-loss
durability, external transactions, or an autonomous distributed scheduler.
See the [measurement and scheduling contract](scheduler-workloads.md).

## Replicated execution extension

Recorded **2026-09-29 at 17:00:10 UTC**, Windows/amd64, Go 1.27.1, from
`TestClusterProcesses` in the working tree based on `f0065a2`, including the
snapshot-history integrity checks delivered with this evidence. The test builds
its own CLI and runs actual localhost TLS connections and independent child
processes; it does not use the simulated fault transport. The machine is shared,
and this is a correctness experiment, not a throughput or availability benchmark.

Raw artifact: [cluster-execution.json](evidence/cluster-execution.json).

| Deliberate event | Observed outcome |
|---|---|
| Start three voters with separate persistent directories | A real Raft leader and quorum accept a keyed job |
| Kill worker1 after its generation-1 claim | Worker2 reclaims after the 2-second lease and commits generation 2 |
| Submit worker1's delayed generation-1 completion | Explicit `stale` rejection; no replacement of the committed result |
| Submit eight more jobs and run two workers | Two simultaneous leases observed; both worker identities commit jobs |
| Attempt worker enqueue and worker identity spoofing | Both rejected by the certificate-bound API |
| Kill the current leader | The surviving quorum retains all nine results; duplicate submission returns the existing job |
| Kill another voter | The minority cannot acknowledge the new submission |
| Restart all three voters from their original directories | Nine completed/retained jobs; zero pending, leased or exhausted |

The interrupted job has a declared 500 ms computation delay; the eight parallel
jobs each have 400 ms. All nine result digests were independently recomputed
from their payloads. Worker1 restarts during the experiment; "two workers" means
two identities and at most two simultaneous worker processes, not two total
process launches. Error replies' default counter fields are not authoritative
queue snapshots; inspect the final successful status response for counts.

Reproduce on Windows with a new output path:

```powershell
$env:FENCELAB_CLUSTER_EVIDENCE = Join-Path (Get-Location) "cluster-evidence.json"
go test ./cmd/fencelab -run "^TestClusterProcesses$" -count=1 -v
Remove-Item Env:FENCELAB_CLUSTER_EVIDENCE
```

The environment variable is optional; without it the test validates behavior
without writing a report. This report is a bounded summary of assertions in
the integration test, not a deterministic replay or a browser-import artifact.

The full Go tests, vet, Windows CLI build and all **50 existing Chromium cases**
passed after the extension. The original 9,000-run v1 report and both v2 search
reports deep-compare identically with the preserved artifacts. Earlier images
are unchanged. Additional tests cover certificate trust separation, role/input
boundaries, historical claim replay, snapshot loss/duplication of receipts,
and the byte integrity/coverage of copied dependency license notices.

A static Linux/amd64 cross-build passed. Compose JSON isolation/mount structure
and PowerShell preparation syntax were checked, but **Docker and Podman were
unavailable**, so no local container run, native Linux run, race result, hosted
deployment, power-loss durability or production certification is claimed.
CI now contains actual container submit/restart checks; configured CI is not a
claim that those future jobs passed. See [operations and limitations](deployment.md).
