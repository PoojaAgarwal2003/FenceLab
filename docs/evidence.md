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
