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

Use small commits tied to a real behavior, check, or documentation change.
Keep benchmark and model claims tied to reproducible evidence. Preserve
third-party notices; project licensing is separate from dependency attribution.
