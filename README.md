# FenceLab

**A lease expires. A worker doesn't.**

FenceLab is a deterministic failure laboratory for distributed job execution.
Pause a worker, let its lease expire, assign a replacement, and watch the old
worker return. Then lose an acknowledgment after a successful write.
What actually prevents the wrong side effect?

The answer is not one mechanism: **leases decide ownership, fencing rejects old
owners, and idempotency prevents repeated logical effects.** FenceLab runs the
same seeded failure schedule under three policies so you can inspect the
difference at the write boundary.

[Run locally](#run-locally) · [The experiment](#the-experiment) ·
[Architecture](docs/architecture.md) · [Next milestones](docs/roadmap.md) ·
[Recorded evidence](docs/evidence.md)

**Milestones 1-5 are implemented:** ownership experiments, programmable faults,
bounded exploration, crash-safe ledger recovery, and a four-process bridge.
The models control delivery timing; this is not a production scheduler or an
autonomously timed distributed cluster.

![FenceLab's failure investigation workbench](docs/images/workbench-desktop.png)

The interface is organized like an investigation notebook: set conditions in a
horizontal control strip, follow the execution timeline, compare the policy
ledger, then inspect the event journal. A light paper palette, serif headings,
and blue/rust annotations distinguish it from a conventional dark dashboard.
See the [mobile layout](docs/images/workbench-mobile.png).

## The experiment

One job, two workers, one lease authority, and an atomic side-effect store.
The first milestone is a **single-process simulation**, not a distributed
cluster or a production scheduler. All displayed milliseconds are virtual.

| Scenario | Lease only | Fencing | Fencing + idempotency |
|---|---|---|---|
| Paused worker returns after reassignment | Stale write and duplicate effect | Old token rejected | Old token rejected |
| Write succeeds, acknowledgment is lost | Old retry and new owner repeat effect | Old retry rejected; new owner still repeats effect | Old retry rejected; new owner gets original result |
| Healthy baseline | One effect | One effect | One effect |

For seed `7` with a `100 ms` virtual lease, accepted effect counts are:

```text
                        lease-only    fenced    fenced-idempotent
paused-worker               2            1              1
lost-ack                    3            2              1
healthy                     1            1              1
```

These are reproduced model outcomes, not throughput measurements.

### The important trade-off

A token attached to a request is not sufficient by itself. In this model,
**storage acknowledges a newer fence before a replacement worker is
dispatched**. That is why an old worker is rejected even if it returns before
the replacement writes. Real implementations must supply that ordered storage
barrier; simply recording the highest token seen on normal writes is weaker.

Fencing also does not deduplicate a valid newer attempt. That requires a
job-scoped effect key checked atomically with the effect. The model assumes
durable fencing and effect-key state. A separate WAL and process bridge now
exercise those boundaries on disk under process crashes; neither retroactively
changes the model into a storage engine. There is no general "exactly-once
execution" claim.

## Run locally

Install **Go 1.27+** on Windows or Linux, then:

```sh
git clone https://github.com/PoojaAgarwal2003/FenceLab.git
cd FenceLab
go run ./cmd/fencelab serve
```

Open **http://127.0.0.1:8091**. Choose a scenario, run the comparison, and select
a policy in the comparison ledger. Step through the trace, scrub virtual time, or export the
comparison as JSON. Ctrl+C stops the server.

No database, Docker, Node.js, API key, or paid service is required to run it.
The Go binary embeds the HTML, CSS, and JavaScript:

```sh
go build -o bin/fencelab ./cmd/fencelab
```

On Windows, use `-o bin/fencelab.exe`. The binary works without a source checkout
or a frontend build. You can select another loopback port with
`serve -listen 127.0.0.1:8092`.

The server is unauthenticated and deliberately binds only to a **numeric
loopback address**. Do not expose it through a public proxy or tunnel.

## Replay from the CLI

```sh
# An intentionally unsafe execution, including its exact violation witnesses.
go run ./cmd/fencelab run -scenario paused-worker -policy lease-only -seed 7

# Same fault schedule, three alternative policies.
go run ./cmd/fencelab compare -scenario lost-ack -seed 7 -lease-ms 100

# 1,000 seeds x 3 scenarios x 3 policies = 9,000 model runs.
go run ./cmd/fencelab check -start-seed 0 -seeds 1000
```

Output is JSON, with no status text mixed into stdout. `run` and `compare` return
exit code `0` when the experiment executes, even if a deliberately unsafe policy
violates an invariant. Inspect `summary.safe` and `violations`.
`check` returns `1` for unexpected outcomes, `2` for invalid input, and `0` when
both the unsafe baselines and protected policy behave as expected.

The first recorded sweep produced **3,000 expected unsafe runs, zero protected
failures, and zero unexpected outcomes**. It explores seeded timings within
three fixed templates, **not every possible interleaving**. See
[raw evidence and scope](docs/evidence.md).

| Option | Meaning |
|---|---|
| `-scenario` | `paused-worker`, `lost-ack`, or `healthy` |
| `-policy` | `lease-only`, `fenced`, or `fenced-idempotent`; `run` only |
| `-seed` | Signed 64-bit schedule seed; default `7` |
| `-lease-ms` | Virtual lease length, `20` to `10000`; default `100` |
| `-start-seed` | First seed for `check`; default `0` |
| `-seeds` | Number of seeds for `check`, `1` to `10000`; default `100` |

The browser/API restrict seeds to JavaScript's exact integer range. Replay
requires the same config **and model version** (`fencelab/v1`).

## Programmable faults and shortest counterexamples

The second laboratory on the page uses **`fencelab/v2`**. Edit the scenario JSON,
delay/drop/duplicate messages, or partition a directed link and heal it later.
Fence installation and its acknowledgment now travel independently.

```sh
go run ./cmd/fencelab network -file examples/eager.json
go run ./cmd/fencelab search -file examples/eager.json -max-states 2000 -max-depth 80 -witness counterexample.json
go run ./cmd/fencelab replay -file counterexample.json
go run ./cmd/fencelab search -file examples/barrier.json -max-states 2000 -max-depth 80
```

The unsafe example produces a **9-decision shortest failure prefix**: the old
worker writes after replacement activation but before the replacement fence
arrives. The same-time write race can also take a safe order. Breadth-first
search explores enabled delivery/timer orders instead of relying on a lucky seed.
The corrected example exhausts its fixed scenario without a violation.

**Important:** v2 distinguishes a *reserved token* from *active ownership*.
The barrier protocol activates ownership only after storage acknowledgment.
It does not magically prevent old writes between token reservation and fence
installation. Its stale-write oracle uses the active epoch; this distinction
is explicitly versioned rather than silently changing v1.

State/depth cutoffs are **inconclusive**, not "safe." Witnesses are shortest
event prefixes within one fixed scenario, not globally minimal fault files.
Export/import them directly in the browser. See
[transport semantics, scenario format, search scope, and CLI exit codes](docs/transport-and-search.md).

![The v2 fault transport and replay laboratory](docs/images/transport-desktop.png)

## Beyond memory: recovery and real processes

The WAL persists epoch reservations, storage fences, and effects together with
their idempotency keys. Every acknowledged mutation follows a checksummed append
and file sync. Recovery rejects complete corrupt records and repairs incomplete
tails. OS file locks prevent concurrent writers.

```sh
# Kill actual child processes at 15 append/sync/apply boundaries.
go run ./cmd/fencelab durability -dir crash-lab > crash-report.json

# Deliver the nine-decision counterexample to four independent OS processes.
go run ./cmd/fencelab bridge -replay docs/evidence/v2-counterexample.json -dir process-lab > process-report.json

# Inspect a recovered ledger (may repair an incomplete tail).
go run ./cmd/fencelab recover -wal crash-lab/write-after-sync.wal
```

Directories must be new; existing files are never overwritten. Import either
JSON report into **"What survives the crash?"** in the dashboard. The server
rechecks the history against the model and the recovery invariants, but never
launches processes or opens WAL paths from browser requests.

The process bridge runs a separate authority, two workers, and an effect store
over private versioned JSON pipes. The parent is a **seeded fault proxy**:
delay/drop/duplicate/partition rules remain controlled virtual deliveries.
Independent actor responses are checked at each operation, not just at the end.
Authority and store are then killed and restarted from their WALs.

The recorded eager prefix commits **one stale effect** and matches the model.
The barrier and lost-result runs each retain **one logical effect**, survive
restart, and match their modeled histories. All 15 crash cases preserve the
required recovery invariants. These are observations, not performance numbers.

**Limits:** the effect is a WAL ledger entry, not an external payment or RPC.
Process-kill tests are not power-loss proof. No quorum, network packet-loss test,
autonomous worker clocks, or recovery of pending delivery queues is implied.
See [durability guarantees](docs/durable-recovery.md),
[the process bridge](docs/process-bridge.md), and [raw evidence](docs/evidence.md).

![The real-process history and recovered ledger inspector](docs/images/process-desktop.png)

See the [mobile process view](docs/images/process-mobile.png) and
[crash-recovery matrix](docs/images/recovery-desktop.png).

## Engineering underneath

- **v1 discrete-event min-heap:** events are ordered by `(virtual time, insertion
  sequence)`. Same-time ordering is explicit and replayable.
- **Monotonic ownership epochs:** an old attempt remains identifiable after
  reassignment. The scheduler cannot recall a request already sent to storage.
- **Storage-side fencing barrier:** reject obsolete attempts at the component
  that actually commits effects, rather than only at the scheduler.
- **Atomic effect-key check:** keep a completed logical operation from being
  repeated by a valid replacement attempt.
- **Trace witnesses:** every violation points to the exact committing event,
  with token, current epoch, owner, and write count.
- **Matched experiments:** policies use the same generated schedule; changing
  protection does not silently change fault timings.
- **v2 programmable transport:** envelopes, directed partition intervals,
  message faults, authoritative timers, and separately delivered fence barriers.
- **Bounded BFS and canonical state hashing:** explore equal-time event choices,
  merge equivalent states, and reconstruct shortest violating prefixes through
  parent links.
- **Framed WAL and CRC32C:** recover monotonic epochs, fences, and effect keys;
  distinguish incomplete tails from corrupt complete records.
- **Process-boundary response checking:** run independent durable actors behind
  the same delivery driver and compare their actual histories with the model.

The model avoids goroutine scheduling, sleeps, wall-clock reads, and global
random state. The real HTTP interface has bounded admission, explicit overload
responses, request timeouts, and graceful shutdown.

## Development

```sh
go test -count=1 ./...
go vet ./...
go build ./...
go test ./internal/sim -run '^$' -bench BenchmarkCompare -benchmem
```

For browser development only, install Node.js 22+:

```sh
npm ci
npx playwright install chromium
npm run test:e2e
```

On Linux, `npx playwright install --with-deps chromium` also installs the
browser's OS dependencies. The suite starts and stops its own real Go server on
port `18091`, exercises desktop/mobile Chromium, and saves demonstration
screenshots under ignored `web-test-results/`.

CI runs Go checks on Windows/Linux, race detection on Linux, and Chromium tests.
The [contributor guide](CONTRIBUTING.md) defines the model's invariants and
evidence expectations.

## Where this goes next

**Milestones 1-5 of 6 are complete; the full advanced project is not finished.**
The [roadmap](docs/roadmap.md) next adds multi-job scheduler workloads, bounded
queues, fairness, retry budgets, and measured overload/recovery behavior.
The current process bridge is intentionally controlled and local.

## Attribution and licensing

See [third-party notices](THIRD_PARTY_NOTICES.md) for Go and browser-test
dependencies, and [architecture references](docs/architecture.md#references)
for the underlying ideas. No project license has been granted yet; upstream
dependency licenses do not automatically license FenceLab.
