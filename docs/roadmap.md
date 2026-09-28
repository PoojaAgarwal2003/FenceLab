# From a failure experiment to a distributed execution laboratory

Each milestone should leave a runnable demonstration, replayable failure,
explicit correctness boundary, and small meaningful commits. Milestones 1-3
are local models; 4-5 add disk recovery and controlled real processes.
Milestone 6 remains future scope.

| Milestone | Engineering work | Demonstration / acceptance |
|---|---|---|
| **1. Ownership experiment - implemented** | Stable virtual-time queue, lease authority, three storage policies, paused-worker and lost-ack faults, seeded checks, local visual replay | Same schedule produces different effect safety; replay JSON is deterministic; healthy baseline does not manufacture failures |
| **2. Programmable fault transport - implemented** | Versioned v2 envelopes; delay/drop/duplicate, delay-driven reorder, directed partition/heal, independently delivered fence acknowledgment | Checked-in scenarios reproduce delayed-barrier failure and corrected active-ownership protocol; worker clocks do not control expiry; v1 is unchanged |
| **3. Bounded model exploration - implemented** | BFS over enabled equal-time deliveries/timers; canonical state hashing; explicit bounds; shortest failure prefixes and JSON replay | 9-decision counterexample; deterministic replay and unmerged-oracle comparison; exhausted vs state/depth-limit outcomes; no general proof or fault-file minimization claim |
| **4. Durable recovery - implemented** | Bounded append-only WAL, CRC32C framing, OS single-writer locks, sync-before-ack, epoch/fence/key recovery, poisoned writers | 15 real child-process kills around append/sync/apply; every torn-tail byte and corruption byte tested; local-ledger and power-loss limits documented |
| **5. Real process bridge - implemented** | Separate authority, workers, and store over versioned private pipes; shared transport interface; seeded virtual fault proxy; independent actor responses | Exact 9-decision counterexample in four OS processes; barrier/lost-result histories match; authority/store forced restart; browser artifact consistency inspector |
| 6. Scheduler workloads | Multiple jobs, bounded queues, priorities/fairness, retry budgets, overload; load and recovery measurements | Measured starvation bounds, completion distributions, rejected load, and recovery costs under declared workloads |

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
report inspector. The bridge controls virtual delivery to real actors; autonomous
network services and wall-clock fault timing are not implemented.

## Next milestone: scheduler workloads

1. Introduce multiple logical job/effect keys, bounded admission, and queue limits.
2. Add fairness/priority policies with explicit starvation and retry budgets.
3. Drive declared overload workloads and measure rejected work, completion
   distributions, throughput, and recovery costs using actual elapsed time.
4. Keep workload measurements separate from virtual model event timestamps.

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
