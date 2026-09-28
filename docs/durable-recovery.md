# Durable recovery: the effect is the ledger

Milestone 4 adds a Windows/Linux single-writer WAL, independent of the existing
v1/v2 simulations. It persists epoch reservations, installed fences, and logical
effects with their idempotency keys. An effect here is **a ledger entry**: this
does not atomically commit an external payment, database write, or HTTP call.

## On-disk contract

Each frame has a 24-byte little-endian header: `FLW1` (4 bytes), strictly
increasing sequence (8), payload length (4), complemented length (4), and CRC32C
over the preceding 20 header bytes plus payload (4). The payload is strict JSON,
at most 4096 bytes. The log is bounded to 16 MiB; there is no compaction yet.

One operation follows:

```text
validate -> header -> payload -> file.Sync -> apply in memory -> acknowledge
              |          |           |              |
          torn tail   unacknowledged operation may survive a process crash
```

Recovery revalidates every transition and sequence. An incomplete final header
or payload is truncated and synced before accepting new work. A complete frame
with a checksum error, invalid header, or invalid transition is an error, even
at EOF; it is never silently discarded. The original corrupt file is preserved.
Checksums detect accidental corruption, not malicious tampering.

Complete unacknowledged frames are recovered and synced. Therefore an operation
may have committed even if its caller saw no acknowledgment. Retrying a logical
effect with the same key returns its original ledger result. A token below the
recovered fence is rejected before deduplication. Reservations advance by exactly
one; after restart, tokens cannot reuse any recovered reservation.

An OS file lock prevents concurrent writers (Linux `flock`, Windows
`LockFileEx`) and is released when the process dies. Append/sync failures poison
the open writer: close and recover rather than continuing with uncertain state.
Concurrent callers within a process are serialized through acknowledgment.

## Durability boundary

The demonstrated fault is **process termination**, not loss of machine power.
`file.Sync` delegates durability to the OS/filesystem/device. Initial directory
creation is not made power-loss durable on all supported platforms. There is no
replication, quorum, bit-rot repair, network-filesystem support, or guarantee
against a device that lies about flush completion. Use a local filesystem and
do not rename/delete an open WAL. File ownership is not distributed coordination.

Tests cut the final record at every byte, corrupt every byte in a two-record
log, restart at append/sync/apply boundaries, retry effects, recover epochs and
fences, and attempt concurrent ownership. The original simulation results remain
unchanged; persistence is a separate implementation rather than retroactive
evidence that the models already implemented crash recovery.

## Kill actual processes

```sh
go run ./cmd/fencelab durability -dir crash-experiment
go run ./cmd/fencelab recover -wal crash-experiment/write-after-sync.wal
```

The first command creates a **new** directory (existing paths are refused),
initializes 15 separate logs, starts a child copy of the executable for each
case, waits for a checkpoint on its stdout pipe, forcibly kills its PID, then
recovers and retries. The matrix is reserve/fence/write crossed with
before-append/after-header/after-append/after-sync/after-apply. The child is
blocked at its checkpoint; this is not a graceful shutdown or a simulated
exception. Artifacts remain on disk, including when a run fails.

JSON output records recovered state, truncation bytes, the next reserved token,
and the retry result. Any violated recovery invariant returns exit 1. Invalid
flags return 2. `recover` requires an existing WAL and may repair its incomplete
tail; it is not a read-only forensic reader. It never ignores complete corrupt
frames. `wal-probe` is the internal child command, not a normal workload tool.

The next reservation must exceed recovered state. All acknowledged baseline
records must survive, and post-sync crashes must preserve the target operation.
An unacknowledged complete append may survive too. For the effect test, a retry
after recovery leaves exactly one logical ledger entry regardless of whether
the original append survived.
