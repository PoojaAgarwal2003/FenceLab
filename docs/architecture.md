# Architecture: ownership is not effect safety

This page describes the preserved **v1** model and original comparison API.
For the independently versioned v2 message transport, reserved/active epoch
distinction, and bounded exploration, see [transport and search](transport-and-search.md).

## Two layers, one deterministic model

```text
Browser controls ---------------------+
                                      |
CLI run / compare / check ----> Config + seeded timing plan
                                      |
                              Discrete-event min-heap
                              (virtual time, sequence)
                                      |
                              Lease authority
                              epoch 1 -> epoch 2
                                      |
                         +------------+------------+
                         |                         |
                      Worker A                  Worker B
                      old attempt               replacement
                         |                         |
                         +------------+------------+
                                      |
                              Side-effect store
                         fence check -> effect-key check
                                      |
                          effects + immutable trace entries
                                      |
                           invariant witnesses + summary
```

These boxes are **modeled components in one Go process**. There is no real
network, broker, storage service, leader election, or cluster in milestone 1.
The actual HTTP server only transports simulation results to a local browser.

## Execution contract

`internal/sim.Run` starts with an empty world and a private seeded PRNG.
The first and second worker durations are in
`[floor(lease/4), 2*floor(lease/4)-1]`. The old request returns at a seeded time
strictly after the first lease deadline and strictly before the second
worker's completion.

The queue orders by virtual time, then insertion sequence. This resolves ties
deterministically, but does **not explore alternative same-time orderings**.
Changing the event model or random-draw order requires a model version change
if exact historical replay must continue to work.

The current templates create at most two attempts:

1. **Paused worker:** A pauses before its side-effect request, the lease expires,
   B is assigned, then A resumes. No lease renewal is modeled.
2. **Lost acknowledgment:** A commits but its acknowledgment is lost. Expiration
   triggers B; A retries its ambiguous request before B writes.
3. **Healthy:** A commits and acknowledges before its lease expires.

A successful current-owner acknowledgment marks the job complete. The
simulation still drains already-scheduled events. A scheduler cannot cancel a
side-effect request merely by deciding a job is finished.

## The fence barrier is an explicit assumption

For protected policies, claim processing is:

```text
increment authoritative epoch
-> raise storage's minimum accepted token
-> receive storage acknowledgment
-> dispatch the worker
```

There is no event interleaving between those model operations. In a real system,
the protocol must durably establish and acknowledge the fence before dispatch.
Issuing an epoch alone cannot atomically invalidate another system's requests.
The availability cost of a delayed or unavailable storage barrier is not
modeled yet; it is a deliberate next-stage fault surface.

This is stronger than "reject tokens lower than the highest token on a previous
normal write." In that weaker scheme, an obsolete writer can reach storage
before the new owner and still be accepted. FenceLab does not credit that weaker
implementation with the stronger guarantee.

The authority is singular and does not crash, epochs never reset, and storage
never loses its fence. There is no claim about split-brain lease issuers or
unsynchronized clocks. The data structures are in memory; durability is a model
assumption rather than implemented disk persistence.

## Fencing and idempotency are different invariants

An effect records its attempt token and the authoritative epoch at commit time.

- **No stale write:** a committed token must not be older than the current
  epoch. The implementation only emits issued tokens; forged future tokens and
  untrusted workers are outside this model.
- **At most one effect:** the stable logical key `invoice-001` may be committed
  once, independent of the number of attempts or their tokens.
- **Completion:** the current owner must eventually acknowledge success in these
  finite templates. This is observed termination, not a general liveness proof.

The protected store checks the fence first, then checks the effect key and commits
atomically. A repeated key returns the original result without a new effect.
Because v1 contains one job, effect-key membership is represented by whether its
single effect record already exists; no general-purpose deduplication database
or expiration policy is implied.

Rejecting an old acknowledgment at the authority does not repair a write that
the unsafe store already committed. Conversely, a higher token does not stop a
legitimate replacement from repeating an already-committed logical operation.

These assumptions only cover effects inside this modeled atomic store.
Sending an email, charging an external API, or publishing into an independent
system requires that effect boundary's own transactional/idempotency support.

## What seeded exploration does

`check` iterates a bounded seed range across three scenarios and three policies,
checks the expected safe/unsafe matrix, and emits the first unsafe witness.
The schedule generator intentionally reaches the dangerous handoff window.
The number of unsafe runs is **not an estimated production failure rate**.

Model tests independently inspect committed effect records to recompute stale
and duplicate counts, verify trace-witness links, preserve event order, and check
that comparison policies share the same plan. Varying timing within a fixed
template does not prove correctness under arbitrary message reorderings,
crashes, storage failures, or other schedules.

## Actual HTTP interface

| Endpoint | Contract |
|---|---|
| `GET /` | Embedded dashboard; no build-time frontend dependency |
| `GET /api/health` | Model version and status |
| `GET /api/compare` | Three complete results for the same config |

Comparison query fields are `seed`, `scenario`, and `lease_ms`. Unknown,
duplicate, empty, malformed, and out-of-range values return HTTP 400; defaults
apply only to omitted fields. Query strings are limited to 256 bytes.
The API accepts only JavaScript-safe integer seeds; the CLI accepts full int64.

Four comparison requests can be active at once. Excess requests receive HTTP
429 and `Retry-After: 1`. Health checks are independent of those slots.
Numeric loopback Host validation, origin/fetch-site checks, a restrictive CSP,
finite server timeouts, and no CORS grant reduce accidental exposure. They are
not authentication or a public-service security design.

The browser applies configuration only when Run is pressed. On a request failure
it explicitly labels any previous results as stale and disables export.
The comparison ledger shows each policy's **complete run outcome**; the timeline
cursor and state boxes show the **selected trace entry**.
The interface places conditions above the execution strip, policy comparisons
beside it on desktop (below it on narrow screens), and a separate event journal
underneath. Keyboard selection retains focus as policy and journal rows update;
the selected journal event exposes `aria-current="step"`. Horizontal timeline
and journal overflow are contained in labeled, keyboard-focusable regions.

## Files worth reading

- `internal/sim/queue.go`: stable event heap.
- `internal/sim/sim.go`: lease transitions, storage checks, and effect witnesses.
- `internal/sim/explore.go`: bounded seeded checks, not a model checker.
- `internal/web/server.go`: local API, admission, embedded files, shutdown.
- `internal/web/static/app.js`: replay state and event-swimlane rendering.
- `tests/laboratory.spec.mjs`: real-server desktop/mobile interaction checks.

## References

- Martin Kleppmann, [How to do distributed locking](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html):
  process pauses, leases, and why the resource must enforce fencing.
- Go, [container/heap](https://pkg.go.dev/container/heap):
  the standard-library priority queue used for the event scheduler.

These are conceptual references, not claims that FenceLab invented fencing,
leases, idempotency, or deterministic simulation.
