# Scheduler workloads: bounded work, explicit trade-offs

Milestone 6 adds a separate single-dispatcher experiment. It does not change v1
or v2 timing, the two-attempt protocol, or the four-process bridge.

## Admission and selection

Jobs have a stable UTF-8 effect key and one of two classes: high priority (0) or
ordinary (1). Capacity bounds **all outstanding jobs**, including the active
attempt. A failed attempt retains its slot and rejoins its class's FIFO tail.
Overload is explicitly rejected, not silently buffered. Admitted keys remain
known after completion/exhaustion; duplicate admission cannot execute a new job.
The whole experiment is bounded to 128 admitted keys.

`fair` uses weighted round robin: three high-priority dispatch slots followed by
one ordinary slot, skipping empty classes. `priority` always prefers high work,
an intentionally starvation-prone comparison policy. Equal-class order is FIFO.
Neither policy accounts for unequal execution cost or preempts an active write.

For fair selection and capacity C, an already queued attempt waits at most
**4C - 1 other dispatches**. There are at most C jobs ahead/in service; every
four actual dispatches offer a slot to a nonempty class. New arrivals and retries
join behind the waiting attempt. A head-of-line ordinary job waits at most
three other dispatches. This is a conservative dispatch bound, **not a
wall-clock latency guarantee**: a blocked disk operation blocks this worker.
Strict priority has no finite bound under continuous high-priority arrivals.

Attempts are capped at 1-5 per admitted job. Exhaustion is a terminal failure,
not successful completion. An exhausted lost-ack job may already have a durable
effect; later sections distinguish acknowledged jobs from committed ledger keys.

## Real elapsed workload runner

```powershell
go run ./cmd/fencelab workload -file examples/workload-overload.json -dir workload-overload > workload-report.json
```

The directory must not exist. Every admitted job uses its own stable
`invoice-NNN` key in `effects.wal`. One dispatcher/worker executes synchronously,
using the fenced-idempotent policy. A global epoch belongs to this **serial
batch**, not an independent lease per job. This is not a distributed scheduler.

Each fourth offered job is ordinary; the others are high priority. Offers are
declared before execution: all at once (`offer_every_ms: 0`) or spaced 1-10 ms.
Admission polls due offers between attempts. It does not backdate admission or
invent concurrent producer threads. Completion latency starts at the declared
offer deadline, so polling lag is included rather than hidden. Each attempt
includes a declared 0-20 ms service delay and, unless failed before execution,
a real synchronous WAL write/deduplication. Timers and filesystem behavior affect
results; `service_ms` is not a claim about actual disk latency.

Faults apply to every Nth offered key: `fail-before-once`, `lost-ack-once`,
or `lost-ack-always`. Retries reuse the key. Lost acknowledgments can exhaust a
job despite its effect already being durable. Completion counts acknowledgments;
effect counts count unique ledger keys. These quantities must not be conflated.

`reopen_after` requests one **clean close/reopen**, followed by a fresh epoch and
durable fence. The in-memory queue survives this probe. Open/replay and new
barrier costs are measured separately. A checkpoint beyond the actual attempt
count is reported as not performed. This is ledger recovery measurement, **not
queue recovery, process death, or crash-recovery throughput**; milestone 4 supplies
the separate forced-process-crash experiments. A cancelled/failed run retains its
WAL for inspection but produces no success report. Cancellation is checked between
operations; it cannot interrupt a stuck filesystem sync.

### What is measured

- Actual monotonic elapsed time from before admission to the drained batch,
  including service waits, ledger operations, idle offer gaps, and reopen probe.
  Initial WAL creation/reserve/fence and final close are excluded.
- Acknowledged jobs per second = completed jobs / actual elapsed seconds.
- Nearest-rank p50/p95/p99 and maximum completion latency for acknowledged jobs
  only; exhaustion and rejection counts are shown separately.
- Initial queue wait from actual admission to first dispatch for executed jobs.
- Dispatch-count fairness, retry and dedup counts, and outstanding-job high water.

The report's compact journal uses negative job indices for admission and positive
indices for dispatch (one-based). Validation replays every selection and retry,
checks the fault declaration, recomputes counters/distributions, and checks the
reopen ledger counts. It establishes **internal consistency, not authenticity**:
an unsigned artifact cannot prove that its elapsed timestamps were measured.
Maximum size stays within the inspector's 64 KiB limit, even at 128 jobs and
five attempts. There is no queue persistence, concurrent worker pool, replicated
metadata, external side effect, compaction, or production latency SLA.
