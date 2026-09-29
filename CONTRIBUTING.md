# Working on FenceLab

Use Go 1.27+. Browser checks additionally need Node.js 22+ and the pinned npm
development dependencies. Follow the [README](README.md#development).

## Invariants before optimizations

- Keep replay deterministic: no wall-clock reads, shared PRNG, sleeps, or
  goroutine scheduling inside `internal/sim` or `internal/model`.
- Preserve `(virtual time, insertion sequence)` event ordering.
- Compare policies against the same failure plan.
- Report a violated invariant with an exact committing trace witness.
- Keep completion and safety separate; an unfinished job is not a success.
- Describe protocol assumptions explicitly. Never treat a fencing token as
  automatically visible to a separate storage system.
- Preserve old model semantics or version a behavior-changing replay format.
- In v2, distinguish reserved tokens from active epochs; do not claim that
  issuing a token instantaneously revokes requests at storage.
- Treat state/depth cutoffs and incomplete jobs explicitly. Compare exploration
  changes against the unmerged reference enumeration and replay artifacts.
- Keep checked-in scenario files equivalent to the browser's built-in examples.
- Keep errors visible, including failed writes, invalid input, overload, and
  browser requests. Never substitute a healthy scenario after an invalid input.
- Acknowledge durable mutations only after sync. Poison a writer after ambiguous
  append/sync errors, reject complete corrupt frames, and preserve effect keys
  exactly across JSON serialization and recovery.
- The bridge's processes are real but its fault schedule is virtual. Keep the
  independent actor implementation and history oracle; never echo model answers.
- Persisted reservations do not preserve active leases. Restarted authorities
  must reserve a fresh epoch and establish the required barrier before activation.
- Imported artifacts prove internal consistency, not trusted provenance. The
  web API must not launch processes or accept filesystem paths to inspect.
- Keep `internal/scheduler/queue.go` clock-free and single-dispatcher. Capacity
  includes active jobs; retries retain their slots and move to the class FIFO tail.
- Keep workload offered deadlines, actual admission, acknowledgment, and durable
  effects distinct. Report exhausted lost-ack jobs even if their effects survived.
- Validate workload selection/counters from the journal and compare generated
  counts to actual WAL responses. Never label virtual time as benchmark latency.
- A workload's clean ledger reopen keeps the queue in memory; it is not a
  forced-process crash, persistent queue, or wall-clock fairness guarantee.
- Keep `internal/cluster` separate from the original lab formats. Apply time is
  leader-supplied and replicated; map iteration must never determine queue order.
- Persist every successful claim receipt, not merely the latest per worker.
  An ancient delayed request must not acquire a different job after restart.
- Commit result, generation check and job completion in one replicated
  transition. Do not claim atomicity for unrelated services or external effects.
- Keep API and peer trust separate. Bind workers to verified certificate
  identities, and never give health probes job privileges. Never add an
  insecure TLS fallback or treat lack of quorum as a successful mutation.

## Before a commit

```sh
go test -count=1 ./...
go vet ./...
go build ./...
npm run test:e2e
```

Run `gofmt` on changed Go files. Browser changes must retain mobile usability,
keyboard controls, and visible error states. Do not commit build outputs,
credentials, npm modules, or arbitrary generated trace dumps.

`TestPhysicalLabs` builds an isolated executable, forcibly kills 15 crash
probes, and runs real-process bridge scenarios. It owns and waits for each PID.
Native WAL file locking supports Windows and Linux; cross-compilation does not
replace native lock/recovery or race testing. Curated reports under
`docs/evidence/` are checked by the artifact API tests.

Browser success-path tests share a single bounded model/artifact executor and run
with one Playwright worker. Explicit overload tests still assert 429 behavior;
do not mask admission failures with automatic success-shaped retries.

`TestClusterProcesses` builds and owns three real coordinator processes plus two
worker identities, then exercises lease recovery, concurrent claims, leader
loss, minority rejection and full disk restart. Set `FENCELAB_CLUSTER_EVIDENCE`
to a **new** JSON path to capture its actual report; it refuses to overwrite.
That format is not accepted by the old browser artifact inspector.

When changing Go dependencies, run `go list -deps` for Windows/amd64 and
Linux/amd64, copy each linked module's actual license/notice/patent files from
its downloaded source, and update `licenses/manifest.json` and
`THIRD_PARTY_NOTICES.md`. Keep original notice bytes; `.gitattributes` disables
line-ending conversion under `licenses/`. `TestDependencyNoticeInventory`
checks artifact hashes and current-platform dependency coverage. Update Go
runtime and Playwright notices when those versions change as well. Do not
infer the project license from a dependency license.

The container CI job generates ephemeral credentials, builds the non-root
image, submits a job, and checks its result after stopping/restarting the entire
topology. Local absence of Docker is not evidence that this job passed.

Use small commits tied to a real behavior, check, or documentation change.
Keep benchmark and model claims tied to reproducible evidence. Preserve
third-party notices; project licensing is separate from dependency attribution.
