# Real processes, controlled delivery

Milestone 5 is a **process bridge**, not an autonomous distributed scheduler.
It runs four actual child processes: authority, worker A, worker B, and store.
The parent acts as a seeded fault proxy and model oracle. Actor implementations
maintain their own state and independently accept/reject commands. They do not
return precomputed model answers.

```text
                  v2 queue / seeded fault proxy / response oracle
                         | private versioned JSON pipes |
                  +------+------------+-----------+----+
                  v                   v           v
              Authority           Workers A/B    Effect store
                  |                                  |
             authority.wal                        store.wal
             reserve + sync                  fence / effect + sync
```

The shared `protocol.Transport` interface carries reserve, fence, acknowledgment,
activation, dispatch, work, write, and result commands. The v2 engine still owns
the virtual queue and authoritative expiry events. Actor responses must match
the model at **every command**, not only the final counts. A separate history
oracle counts committed effects and compares their tokens with active ownership.

## Run it

```sh
# Reproduce the exact shortest stale-write counterexample in four processes.
go run ./cmd/fencelab bridge -replay docs/evidence/v2-counterexample.json -dir bridge-eager

# Choose equal-time orders by seed while retaining the scenario's faults.
go run ./cmd/fencelab bridge -file examples/barrier.json -seed 7 -dir bridge-barrier
go run ./cmd/fencelab bridge -file examples/lost-result.json -seed 11 -dir bridge-retry
```

Each directory must be new. No existing experiment is overwritten or deleted.
Stdout is one JSON report; diagnostics go to stderr. Exit 0 means the real
responses and model agreed, **including intentional unsafe outcomes**. Inspect
`observed.safe`, `matches_model`, and `restart_verified`. Invalid arguments/input
return 2; failed processes, response mismatches, or recovery failures return 1.
The run has a 30-second process deadline and the v2 event cap.

All four actors complete an initial inspection handshake before execution.
Pipes carry bounded newline-delimited strict JSON with a version and increasing
request sequence; malformed input or persistence errors close the actor.
No listening ports, API keys, containers, remote services, or dependencies are
needed. The internal `actor` command is not intended as a standalone service.

## Faults and observations

Delay/drop/duplicate/partition/heal rules act in the **shared virtual delivery
queue**, before commands reach the process boundary. The seed chooses among
enabled equal-time events. An exact replay instead uses the recorded event IDs.
A returned worker `ready` response permits the driver to enqueue its write.
Store acknowledgments follow durable append/sync; a dropped result does not
undo an effect. An old write can still commit when eager activation overtakes
installation of a newer fence.

This deliberately decouples reproducible protocol faults from OS scheduling.
It does not inject TCP packet loss, benchmark network latency, replay identical
wall-clock timings, or make workers run their own lease clocks. Worker clock
skews remain model annotations. This is a bridge from the model to real process
and disk boundaries, not a claim to have built a replicated cluster.

## Crash and restart

After executing the selected schedule/prefix, the parent forcibly kills the
authority and store PIDs, recovers their logs, starts replacements, and checks
recovered state through fresh process handshakes. It also reserves a strictly
newer token in the restarted authority. This recovery probe is separate from
the modeled history; pending model messages are not resumed after restart.

Active leases, pending work, and completion acknowledgments are not persisted.
A restarted authority cannot reactivate an epoch from its recovered log: it must
reserve a fresh one and, with the barrier policy, receive its fence acknowledgment.
The store recovers the exact ledger effects and keys. The process-kill matrix in
[durable recovery](durable-recovery.md) separately tests retries at all five
commit boundaries.

`bridge.Check` replays the recorded requests/responses against v2, recomputes
invariants, and verifies recovered ledger records. Importing a matching JSON
report proves **internal consistency**, not that a trusted machine produced it.
There is no report signature or general distributed linearizability proof.
