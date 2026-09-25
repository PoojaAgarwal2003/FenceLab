# Milestone 1 evidence

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

## Screenshots

The [desktop](images/laboratory-desktop.png) and
[mobile](images/laboratory-mobile.png) images come directly from the first browser
test against the real Go server. They show model results for the paused-worker
scenario at seed `7` and virtual lease `100 ms`; no metric was painted over.

To refresh them, run `npm run test:e2e`, review the two `laboratory.png` files
under `web-test-results/`, and copy the intended captures into `docs/images/`.
Do not present virtual event times as measured application latency.
