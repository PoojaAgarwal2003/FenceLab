# Message faults and bounded exploration

Milestones 2 and 3 add **`fencelab/v2`**, an independent message-delivery model.
The original v1 seeded engine and its CLI commands are unchanged. Both remain
single-process simulations: no real packets, processes, disk recovery, or
consensus protocol are being tested.

## Why a separate model version?

v1 made issuance, fence installation, and activation one non-interleaved
operation. v2 models the messages between those operations:

```text
Authority                       Store                        Worker
reserve token ---- fence ------> install minimum token
              <--- fence-ack ---
activate ownership
              --------------------- dispatch --------------> work
                                <------ write / retry -------
                                commit or deduplicate
              <---- result -----
acknowledge job
```

Every queued envelope has a stable event/message ID, kind, sender, receiver,
token, and logical key. Sends and deliveries appear separately in the trace.
Timers are queued events too. The model schedules at most two attempts and
one retry per started attempt; duplicate dispatches do not start extra work.
Results go directly to the authority in this abstraction, not through a
separately modeled worker acknowledgment hop.

### Reserved token is not active ownership

The barrier protocol reserves a token, installs a monotonically increasing
storage fence, waits for its acknowledgment, and **only then activates the
replacement and starts its authoritative lease**. Until then, the previous
owner remains the last active owner, although its lease may have expired.
The eager protocol intentionally activates and dispatches before acknowledgment.

This distinction is essential: issuance at an authority cannot instantly revoke
an in-flight request at a separate store. While the fence is delayed, the old
writer can still commit. Under the barrier protocol this is before replacement
activation; under eager activation it can be a stale write.

The v2 stale-write oracle is **committed token < active epoch**, not reserved
epoch, wall-clock lease expiry, or the largest token ever issued. This is a
deliberately versioned semantic boundary, not a proof that a barrier prevents
all writes after token reservation or expiry. The trace distinguishes
`epoch_issued` from `ownership_activated`. Dropping a fence acknowledgment
can leave the job incomplete: safety is not availability.

Storage's check/deduplication/commit is atomic in memory. Persistent fencing
and effect-key state are still assumptions. The idempotency key is one logical
job, `invoice-001`, shared by both attempts.

## Scenario format

Start with [the unsafe race](../examples/eager.json),
[the barrier protocol](../examples/barrier.json),
[a partition/heal](../examples/partition-heal.json), or
[lost results and duplicated writes](../examples/lost-result.json).

Required scalar fields: `version`, `name`, `policy`, `protocol`, `lease_ms`,
`work_ms`, and `latency_ms`. Missing `clock_skew_ms` means `[0,0]`; if supplied,
it must contain exactly two integers. Missing/empty faults or partitions mean
none. Unknown fields, duplicate keys at any depth, trailing JSON values,
invalid types, more than 32 nesting levels, and input over 64 KiB are rejected.

| Setting | Bound / meaning |
|---|---|
| `policy` | `lease-only`, `fenced`, or `fenced-idempotent` |
| `protocol` | `barrier` or intentionally unsafe `eager`; lease-only has no fence |
| `lease_ms` | 20-1000 virtual milliseconds, starting at activation |
| `work_ms` | 1 through `lease_ms - 1` |
| `latency_ms` | 1-100 virtual milliseconds for each message |
| `clock_skew_ms` | Worker A/B clock offsets, each -10000 to 10000; displayed at send time only |
| `faults`, `partitions` | Up to 16 of each |

Worker-local clock values **never control authoritative expiry**. Changing
clock offsets alone preserves the schedule and committed effects.

### Fault rules

Rules match `kind`, with optional `from`, `to`, and `token`. Empty endpoints
match any; token `0` matches either attempt. Supported message kinds are
`fence`, `fence-ack`, `dispatch`, `write`, and `result`.
**First matching rule wins**, for every matching send (including retries).

- `delay`: add `delay_ms` (1-10000) to the base latency. Delaying selected messages
  reorders delivery; there is no artificial "deliver before send" operation.
- `drop`: do not enqueue delivery; `delay_ms` must be zero.
- `duplicate`: enqueue one additional copy. `delay_ms` (0-10000) is the extra
  copy's spacing, not a delay applied to both copies.

Partitions are directional `from`/`to` links with `[start_ms, end_ms)` intervals.
Delivery within a partition is **held until healing**, not dropped; overlapping
windows extend the hold until the link is usable. Window endpoints are bounded
by 20000 virtual milliseconds. The timeline records partition/heal events and
held deliveries. Delays also apply to messages already in flight whose planned
arrival falls inside the declared window.

## Run, search, and replay

```sh
go run ./cmd/fencelab network -file examples/eager.json
go run ./cmd/fencelab search -file examples/eager.json -max-states 2000 -max-depth 80 -witness counterexample.json
go run ./cmd/fencelab replay -file counterexample.json
go run ./cmd/fencelab search -file examples/barrier.json -max-states 2000 -max-depth 80
```

`-witness` creates a file only when a counterexample is found, and refuses to
overwrite an existing path. A replay contains the complete scenario, version,
and ordered event IDs. Every decision must be currently enabled; invalid,
future, or unknown decisions are rejected rather than silently replaced.

`network` uses earliest virtual time, then lowest event ID. It drains the queue
even after a current-owner result. `replay` executes exactly its saved prefix,
without silently completing the schedule. Inspect `halt_reason` (`quiescent`,
`prefix`, or `event-limit`), `pending`, `summary.safe`, and `summary.completed`
separately. A safe prefix is not a safe complete execution.

Normal network runs are capped at 256 event decisions. Search bounds are
1-10000 stored states and depth 1-128; defaults are 2000 and 80.

### What the explorer really does

Breadth-first search branches on **every event at the earliest pending virtual
time**. Equal-time deliveries and timers can race. A fixed fault file determines
arrival times; search does not invent arbitrary latency, loss, or crashes.

Canonical SHA-256 state keys include current time, next ID, issued token,
active ownership, fence, completion, per-attempt state, pending envelopes, and
committed effects. Display traces and counters that cannot affect future safety
are excluded. Equivalent states are merged. Parent links retain replay paths
without retaining each expanded machine snapshot.

The first counterexample is a **shortest event-decision prefix** for that
scenario. This removes irrelevant suffixes through BFS, rather than returning
an entire run. It does **not** shrink fault definitions, find a globally
smallest scenario, or minimize across different clock/delay settings.

| Search status | Interpretation |
|---|---|
| `counterexample` | Exact shortest violating prefix, with witness and trace |
| `exhausted` | Explored all reachable enabled interleavings for this finite scenario |
| `state-limit` | Exploration stopped before completion; inconclusive |
| `depth-limit` | At least one nonterminal branch was cut off; inconclusive |

`terminal_states` and `incomplete_jobs` count distinct terminal states, not
probabilities or all execution schedules. A search can exhaust with incomplete
jobs, including a permanently lost barrier acknowledgment.

CLI exit codes: `0` for an executed experiment/search (including an intentional
counterexample), `3` for search state/depth cutoffs, `2` for invalid input, and
`1` for cancellation or output/witness-write failure. Inspect the JSON result;
exit `0` is not an assertion that a scenario is safe.

## Browser and HTTP

The second laboratory on the main page includes a strict JSON scenario editor,
fault examples, explicit search bounds, a network timeline, a scrubber and
event journal, and witness export/import. v1 controls above it stay independent.
Failed requests label previous results stale and disable artifact export.

| Endpoint | Request |
|---|---|
| `GET /api/v2/examples` | Built-in versioned scenarios |
| `POST /api/v2/run` | Scenario JSON |
| `POST /api/v2/search` | `{ "scenario": ..., "bounds": { "max_states": 2000, "max_depth": 80 } }` |
| `POST /api/v2/replay` | Exported replay JSON |

POST endpoints require JSON, reject query parameters, and limit bodies to
64 KiB. One v2 operation is admitted at a time, independently of v1 comparison
slots; excess receives HTTP 429. Searches share a three-second request context
budget and return explicit timeout errors. Existing numeric-loopback,
Host/origin, CSP, and server-timeout restrictions still apply. No public
deployment or authentication is implied.

## Verification and remaining scope

Tests independently recompute invariant counts from committed effects, replay
artifacts exactly, compare BFS to unmerged enumeration, verify shortest depth,
exercise a same-time race with both safe and unsafe outcomes, and check state
merging, bounds, cancellation, strict parsing, and transport faults. Browser
tests round-trip downloaded witnesses through the real API.

See [recorded evidence](evidence.md). Durable recovery, separate real worker
processes, and multi-job scheduler workloads remain milestones 4-6.
