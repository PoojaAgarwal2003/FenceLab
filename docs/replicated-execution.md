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

## Remote execution and API authorization

Milestone 8 adds actual HTTPS worker processes, not the controlled virtual-time
bridge. Each API connection requires a verified client certificate. The leaf's
signed organizational-unit role is `admin`, `worker`, or health-only `probe`.
Admins submit/query jobs;
workers claim/complete only as their certificate common name. A worker cannot
claim another identity, enqueue work, or use its API certificate on Raft.

Clients retry only their configured HTTPS endpoints and never follow redirects.
Keys and successful claim request IDs remain in the replicated state, including
historical receipts, so even a late duplicate from an older request cannot
consume new work. Unknown/duplicate JSON fields, body overflow, browser-origin
requests, and role mismatches fail explicitly. Each node admits at most 16 API
operations concurrently and returns 429 when busy. File/network failures are
not converted to acknowledged results.

TLS 1.3 is mandatory. Bootstrap PKI creates two independent issuers, three node
identities, two worker identities, one admin identity, and one health-only probe
identity. Certificates expire after 90 days and cover `node1`/`node2`/`node3`,
`localhost`, and `127.0.0.1`.
These are local bootstrap credentials; use an organization-managed PKI with the
correct host SANs for real hosts. Keep issuer private keys offline. On Windows,
restrict the directory ACL yourself; POSIX mode 0600 is not a Windows ACL.

Build once:

```powershell
go build -o bin/fencelab.exe ./cmd/fencelab
.\bin\fencelab.exe cluster-pki -dir pki
$peers = "node1=127.0.0.1:7001,node2=127.0.0.1:7002,node3=127.0.0.1:7003"
```

Start each node in a separate terminal, from the repository:

```powershell
.\bin\fencelab.exe cluster-node -id node1 -dir cluster-data/node1 -credentials pki/node1 -raft-listen 127.0.0.1:7001 -api-listen 127.0.0.1:8101 -peers $peers -bootstrap
.\bin\fencelab.exe cluster-node -id node2 -dir cluster-data/node2 -credentials pki/node2 -raft-listen 127.0.0.1:7002 -api-listen 127.0.0.1:8102 -peers $peers
.\bin\fencelab.exe cluster-node -id node3 -dir cluster-data/node3 -credentials pki/node3 -raft-listen 127.0.0.1:7003 -api-listen 127.0.0.1:8103 -peers $peers
```

Set `$peers` in each terminal; variables do not propagate between shells.
Use a separate data directory for every voter. Linux uses the same flags with
`bin/fencelab` instead of the `.exe` path.

In two further terminals, start workers with distinct credentials:

```powershell
$endpoints = "https://127.0.0.1:8101,https://127.0.0.1:8102,https://127.0.0.1:8103"
.\bin\fencelab.exe cluster-worker -credentials pki/worker1 -endpoints $endpoints
.\bin\fencelab.exe cluster-worker -credentials pki/worker2 -endpoints $endpoints
```

Then submit, retry the same submission, and query the result:

```powershell
.\bin\fencelab.exe cluster-client -credentials pki/admin -endpoints $endpoints -operation enqueue -file examples/cluster-job.json
.\bin\fencelab.exe cluster-client -credentials pki/admin -endpoints $endpoints -operation status -key invoice-2026-001
```

Set `$endpoints` in every terminal that uses it. Workers emit JSON event lines;
nodes emit operational logs on stderr. Worker computation is the declared delay
followed by SHA-256, never an arbitrary executable. Leases default to 5 seconds;
there is no renewal yet, so choose a lease longer than the job plus network margin.
A killed worker's lease expires and its job is reclaimable until its attempt
budget is exhausted. A timed-out response is ambiguous; retry the same key.

`/health/live` means the API process responds; `/health/ready` requires current
quorum-confirmed leadership. Followers may be alive but not writable. Both need
mTLS. SIGINT/SIGTERM closes HTTP and Raft gracefully; process-kill recovery is
tested separately. The original `serve` dashboard remains loopback-only and
does not submit commands into this cluster.
