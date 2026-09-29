# Scheduler workloads: bounded work, explicit trade-offs

Milestone 6 adds a separate single-dispatcher experiment. It does not change v1
or v2 timing, the two-attempt protocol, or the four-process bridge.

## Admission and selection

Jobs have a stable UTF-8 effect key and one of two classes: high priority (0) or
ordinary (1). Capacity bounds **all outstanding jobs**, including the active
attempt. A failed attempt retains its slot and rejoins its class's FIFO tail.
Overload is explicitly rejected, not silently buffered. Admitted keys remain
known after completion/exhaustion; duplicate admission cannot execute a new job.
The whole experiment is bounded to 128 admitted keys.

`fair` uses weighted round robin: three high-priority dispatch slots followed by
one ordinary slot, skipping empty classes. `priority` always prefers high work,
an intentionally starvation-prone comparison policy. Equal-class order is FIFO.
Neither policy accounts for unequal execution cost or preempts an active write.

For fair selection and capacity C, an already queued attempt waits at most
**4C - 1 other dispatches**. There are at most C jobs ahead/in service; every
four actual dispatches offer a slot to a nonempty class. New arrivals and retries
join behind the waiting attempt. A head-of-line ordinary job waits at most
three other dispatches. This is a conservative dispatch bound, **not a
wall-clock latency guarantee**: a blocked disk operation blocks this worker.
Strict priority has no finite bound under continuous high-priority arrivals.

Attempts are capped at 1-5 per admitted job. Exhaustion is a terminal failure,
not successful completion. An exhausted lost-ack job may already have a durable
effect; later sections distinguish acknowledged jobs from committed ledger keys.
