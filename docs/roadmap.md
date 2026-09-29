# From a failure experiment to a distributed execution laboratory

Each milestone should leave a runnable demonstration, replayable failure,
explicit correctness boundary, and small meaningful commits. Milestones 1-3
are local models; 4-5 add disk recovery and controlled real processes.
Milestone 6 adds bounded multi-job workloads with measured elapsed time.
All six planned milestones are implemented within the boundaries below.

| Milestone | Engineering work | Demonstration / acceptance |
|---|---|---|
| **1. Ownership experiment - implemented** | Stable virtual-time queue, lease authority, three storage policies, paused-worker and lost-ack faults, seeded checks, local visual replay | Same schedule produces different effect safety; replay JSON is deterministic; healthy baseline does not manufacture failures |
| **2. Programmable fault transport - implemented** | Versioned v2 envelopes; delay/drop/duplicate, delay-driven reorder, directed partition/heal, independently delivered fence acknowledgment | Checked-in scenarios reproduce delayed-barrier failure and corrected active-ownership protocol; worker clocks do not control expiry; v1 is unchanged |
| **3. Bounded model exploration - implemented** | BFS over enabled equal-time deliveries/timers; canonical state hashing; explicit bounds; shortest failure prefixes and JSON replay | 9-decision counterexample; deterministic replay and unmerged-oracle comparison; exhausted vs state/depth-limit outcomes; no general proof or fault-file minimization claim |
| **4. Durable recovery - implemented** | Bounded append-only WAL, CRC32C framing, OS single-writer locks, sync-before-ack, epoch/fence/key recovery, poisoned writers | 15 real child-process kills around append/sync/apply; every torn-tail byte and corruption byte tested; local-ledger and power-loss limits documented |
| **5. Real process bridge - implemented** | Separate authority, workers, and store over versioned private pipes; shared transport interface; seeded virtual fault proxy; independent actor responses | Exact 9-decision counterexample in four OS processes; barrier/lost-result histories match; authority/store forced restart; browser artifact consistency inspector |
| **6. Scheduler workloads - implemented** | Multiple keyed jobs, bounded outstanding admission, weighted fair/strict-priority dispatch, FIFO retries with budgets, declared faults and overload | Dispatch-count fairness bound and starvation control; measured completion distributions, rejected load and ledger reopen costs; replay-validated report inspector |

## Completed milestone 2-3 artifacts

[Scenario files](../examples/eager.json), [protocol and search contracts](transport-and-search.md),
[recorded evidence](evidence.md), CLI `network`/`search`/`replay`, and a browser
scenario editor, fault timeline, event journal, and witness export/import.

Shortest-prefix generation fulfills the counterexample-reduction step within
the fixed scenario's event-decision space. Reducing fault files themselves and
exploring arbitrary delivery times are not implemented.

## Completed milestone 4-5 artifacts

[WAL and crash boundaries](durable-recovery.md), [process bridge](process-bridge.md),
[recorded evidence](evidence.md), CLI `durability`/`recover`/`bridge`, and a browser
report inspector. The bridge controls virtual delivery to real actors; it does
not acquire autonomous network semantics from the separate extension below.

## Completed milestone 6 artifacts

[Scheduler contracts](scheduler-workloads.md), four declared
[workload configurations](../examples/workload-overload.json), CLI `workload`,
four [measured reports](evidence.md#milestone-6---2026-09-29), and the browser's
queue pressure, dispatch order, completion, and recovery views.

The queue is in memory and the worker is serial. Capacity includes in-flight
work. Weighted fair selection has a conservative per-attempt dispatch bound;
strict priority intentionally has none under continuous high-priority arrivals.
Completion distributions use actual elapsed time, not v1/v2 event timestamps.
The recovery probe closes/reopens the effect ledger, not a durable queue.

## Implemented execution extension

These are four additional repository milestones, not retroactive changes to
the original six laboratories.

| Milestone | Implemented scope | Acceptance and limits |
|---|---|---|
| **7. Persistent replicated queue** | Deterministic keyed job FSM, weighted selection, durable receipts, three-voter HashiCorp Raft, bbolt, snapshots and separate peer PKI | Real network failover, majority-only acknowledgments, disk restart; fixed membership and finite retention |
| **8. Concurrent remote workers** | Role-checked HTTPS/mTLS APIs, independent worker processes, ambiguous-response retries, worker identity binding and generation-fenced results | Actual worker/leader kills, simultaneous leases, stale rejection and full-cluster recovery; SHA-256 workload, not arbitrary external effects |
| **9. Deployment and operations** | Non-root scratch image, isolated Compose networks, per-node volumes, scoped credentials, health-only probes, resource limits and operator runbook | Linux static cross-build and configuration checks passed locally; Docker unavailable, runtime smoke is configured in CI, not claimed as observed |
| **10. Evidence and attribution** | Actual process evidence, preserved model regressions, full dependency notices/provenance, integrity checks, CI and scope documentation | Repository acceptance checked on Windows; native Linux/race/container results await their runners |

See [queue/API contracts](replicated-execution.md),
[deployment and recovery procedures](deployment.md),
[recorded execution evidence](evidence.md#replicated-execution-extension),
and [upstream notices](../THIRD_PARTY_NOTICES.md).

Production SLO certification, public hosting, multi-host operations, membership
changes, automated certificate lifecycle, online backups, unlimited key
retention, external effect transactions and FenceLab's own license selection
remain explicitly outside this reference deployment's completed scope.

## Boundaries to keep

- No managed queue or database hides the algorithm under study.
- A simulation of replication is not a consensus implementation. Multiple
  authorities require a separately designed, tested coordination protocol.
- Random seed sweeps are not exhaustive model checking.
- Virtual time is not a latency benchmark.
- Atomic in-memory operations are not evidence of crash durability.
- A project demo is not a promise of production-safe exactly-once execution.
- Keep the entire core project local and free to run. Containers are optional
  deployment tooling; paid hosted infrastructure is not a prerequisite.
