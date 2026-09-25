# From a failure experiment to a distributed execution laboratory

Each milestone should leave a runnable demonstration, replayable failure,
explicit correctness boundary, and small meaningful commits. This roadmap is
future scope, not a list of already-implemented features.

| Milestone | Engineering work | Demonstration / acceptance |
|---|---|---|
| **1. Ownership experiment - implemented** | Stable virtual-time queue, lease authority, three storage policies, paused-worker and lost-ack faults, seeded checks, local visual replay | Same schedule produces different effect safety; replay JSON is deterministic; healthy baseline does not manufacture failures |
| 2. Programmable fault transport | Explicit send/deliver events; delay, drop, duplicate, reorder, partition/heal; independently scheduled fence acknowledgment | A scenario file reproduces a delayed-barrier bug; local clocks cannot masquerade as authoritative expiry |
| 3. Bounded model exploration | Branch on enabled event deliveries; state hashing, search bounds, counterexample shrinking; distinguish found failure from exhausted search | Smallest reproducible stale-write schedule; reported state/depth bounds; no blanket proof claim |
| 4. Durable recovery | Append-only WAL with framing/checksums; recover epochs, fences, and effect keys; crash injection around flush/commit points | Kill/restart without epoch reuse or duplicate committed effects; torn-tail handling tested; atomicity/durability boundaries documented |
| 5. Real process bridge | Separate authority, workers, and effect store using explicit protocols; transport interface shared with simulation; seeded fault proxy | Run a modeled counterexample against real local processes; compare histories rather than claiming timing equivalence |
| 6. Scheduler workloads | Multiple jobs, bounded queues, priorities/fairness, retry budgets, overload; load and recovery measurements | Measured starvation bounds, completion distributions, rejected load, and recovery costs under declared workloads |

## Next milestone: four independently useful tasks

1. Define a versioned scenario format with strict validation and a transport
   envelope carrying sender, receiver, message ID, token, and logical key.
2. Make delivery ordering and network faults explicit in the event queue while
   keeping existing v1 replay semantics available.
3. Separate epoch allocation from storage-barrier acknowledgment; demonstrate
   the dispatch-before-barrier counterexample and its corrected protocol.
4. Add network lanes and fault controls to the UI, retain a replay artifact for
   each failure, and run the old scenarios as regression baselines.

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
