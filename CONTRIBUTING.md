# Working on FenceLab

Use Go 1.27+. Browser checks additionally need Node.js 22+ and the pinned npm
development dependencies. Follow the [README](README.md#development).

## Invariants before optimizations

- Keep replay deterministic: no wall-clock reads, shared PRNG, sleeps, or
  goroutine scheduling inside `internal/sim`.
- Preserve `(virtual time, insertion sequence)` event ordering.
- Compare policies against the same failure plan.
- Report a violated invariant with an exact committing trace witness.
- Keep completion and safety separate; an unfinished job is not a success.
- Describe protocol assumptions explicitly. Never treat a fencing token as
  automatically visible to a separate storage system.
- Preserve old model semantics or version a behavior-changing replay format.
- Keep errors visible, including failed writes, invalid input, overload, and
  browser requests. Never substitute a healthy scenario after an invalid input.

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

Use small commits tied to a real behavior, check, or documentation change.
Keep benchmark and model claims tied to reproducible evidence. Preserve
third-party notices; project licensing is separate from dependency attribution.
