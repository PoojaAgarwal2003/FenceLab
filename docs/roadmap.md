# From a failure experiment to a distributed execution laboratory

Each milestone should leave a runnable demonstration, replayable failure,
explicit correctness boundary, and small meaningful commits. Milestones 1-3
are implemented as local models; milestones 4-6 remain future scope.

| Milestone | Engineering work | Demonstration / acceptance |
|---|---|---|
| **1. Ownership experiment - implemented** | Stable virtual-time queue, lease authority, three storage policies, paused-worker and lost-ack faults, seeded checks, local visual replay | Same schedule produces different effect safety; replay JSON is deterministic; healthy baseline does not manufacture failures |
| **2. Programmable fault transport - implemented** | Versioned v2 envelopes; delay/drop/duplicate, delay-driven reorder, directed partition/heal, independently delivered fence acknowledgment | Checked-in scenarios reproduce delayed-barrier failure and corrected active-ownership protocol; worker clocks do not control expiry; v1 is unchanged |
| **3. Bounded model exploration - implemented** | BFS over enabled equal-time deliveries/timers; canonical state hashing; explicit bounds; shortest failure prefixes and JSON replay | 9-decision counterexample; deterministic replay and unmerged-oracle comparison; exhausted vs state/depth-limit outcomes; no general proof or fault-file minimization claim |
| 4. Durable recovery | Append-only WAL with framing/checksums; recover epochs, fences, and effect keys; crash injection around flush/commit points | Kill/restart without epoch reuse or duplicate committed effects; torn-tail handling tested; atomicity/durability boundaries documented |
| 5. Real process bridge | Separate authority, workers, and effect store using explicit protocols; transport interface shared with simulation; seeded fault proxy | Run a modeled counterexample against real local processes; compare histories rather than claiming timing equivalence |
| 6. Scheduler workloads | Multiple jobs, bounded queues, priorities/fairness, retry budgets, overload; load and recovery measurements | Measured starvation bounds, completion distributions, rejected load, and recovery costs under declared workloads |

## Completed milestone 2-3 artifacts

[Scenario files](../examples/eager.json), [protocol and search contracts](transport-and-search.md),
[recorded evidence](evidence.md), CLI `network`/`search`/`replay`, and a browser
scenario editor, fault timeline, event journal, and witness export/import.

Shortest-prefix generation fulfills the counterexample-reduction step within
the fixed scenario's event-decision space. Reducing fault files themselves and
exploring arbitrary delivery times are not implemented.

## Next milestone: durable recovery

1. Define a versioned WAL record format with framing, checksums, and explicit
   flush/commit acknowledgment semantics.
2. Persist and recover reserved epochs, installed fences, and logical effect
   keys without assuming in-memory state survives a crash.
3. Inject crashes before/after append, flush, effect commit, and acknowledgment;
   distinguish torn tails from middle-of-log corruption.
4. Replay recovery histories against the model and document the actual storage
   durability guarantees. No real-process bridge until these boundaries hold.

## Boundaries to keep

- No managed queue or database hides the algorithm under study.
- A simulation of replication is not a consensus implementation. Multiple
  authorities require a separately designed, tested coordination protocol.
- Random seed sweeps are not exhaustive model checking.
- Virtual time is not a latency benchmark.
- Atomic in-memory operations are not evidence of crash durability.
- A project demo is not a promise of production-safe exactly-once execution.
- Keep the entire core project local and free to run. Containers and hosted
  infrastructure are optional future tooling, not prerequisites.
