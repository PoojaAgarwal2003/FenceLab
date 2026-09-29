# Replicated execution, separate from the experiments

Milestone 7 introduces a real three-voter Raft state machine. The original
six laboratories keep their formats, semantics, and local dashboard.

## What is built here versus reused

FenceLab implements keyed admission, two-class weighted-fair claim selection,
per-job ownership generations, retry budgets, durable claim receipts, result
validation, and snapshot invariants. HashiCorp Raft supplies consensus/elections
and snapshot coordination; raft-boltdb and bbolt supply synchronous persistent
log/stable storage. FenceLab does **not** claim to have invented or implemented
the consensus algorithm. Versions are pinned in `go.mod`/`go.sum`; licenses are
tracked in [third-party notices](../THIRD_PARTY_NOTICES.md).

```text
client submission ------> current leader ------> majority replicated log
                               |                        |
worker claim / completion -----+                        v
                                               identical queue FSMs
                                                /      |      \
                                            node1    node2    node3
                                            local    local    local
                                            disk     disk     disk
```

An enqueue key and its immutable specification are stored together. Retrying the
same submission returns the existing job; changing the specification conflicts.
At most 256 pending/leased jobs and 4,096 retained keys are allowed. Completed
keys are deliberately not garbage-collected, because doing so would erase
deduplication history. The finite key limit is an explicit operating boundary,
not an unbounded production queue. A fresh cluster is a new deduplication domain.

Every worker has at most one live claim. A stable request ID makes lost claim
responses retryable without silently leasing another job. Jobs receive a newer
generation on each claim; expired claims rejoin their class's FIFO tail or
exhaust their attempt budget. The weighted selector offers three high-priority
slots followed by one ordinary slot. This balances **claims**, not CPU time;
unequal runtimes and concurrent workers prevent a wall-clock fairness promise.

Completion checks worker identity, generation, lease status, and the expected
SHA-256 of the submitted payload. The result ledger and terminal job state are
one replicated transition. Repeating a committed completion returns the original
result. This is a deliberately bounded, verifiable computation, **not arbitrary
shell execution** and not an external exactly-once transaction.

Leader timestamps are replicated commands, clamped to a nondecreasing FSM clock.
Clock jumps may expire jobs early or delay takeover. Generation checks protect
the ledger even then. No lease timer from the simulation is reused. Expiry is
processed by subsequent queue commands; it is not an independently persisted
timer service. API reads use a committed Raft barrier, not an unchecked follower.

## Persistence and quorum boundaries

The log and Raft term/vote metadata use bbolt with sync enabled. Snapshots include
jobs, results, queue order, selector cursor, clock, and claim receipts. Restore
validates ownership and capacity invariants. Raft snapshots compact the log;
they do not remove completed job keys.

Only node1 bootstraps a **new** three-voter cluster. Each node has its own data
directory, fixed identity/topology manifest, and exclusive database writer.
Existing storage is reopened, never re-bootstrapped. One failed voter leaves a
quorum; two failed voters must stop successful writes. No unsafe force-recovery
or automatic new-cluster fallback is implemented.

Consensus traffic uses TLS 1.3 with mutual authentication, with a separate CA
from API clients. Neither HTTP workers nor the browser can speak unauthenticated
Raft. The storage assumes reliable local filesystems and non-Byzantine voters;
hardware power-loss guarantees, corrupted-majority repair, membership changes,
cross-region tuning, and production SLO certification are not supplied.
