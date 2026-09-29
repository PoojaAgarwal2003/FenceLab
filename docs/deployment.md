# Deploying the replicated execution mode

This is an operable **single-host reference deployment**, not a claim of
production readiness or host-level high availability. It runs three persistent
voters and two concurrent workers with real mTLS networking. Docker/Podman was
not installed on the development machine: native process execution is verified,
but this container topology must pass the supplied CI smoke job before use.
Do not expose the original unauthenticated laboratory dashboard.

## Bootstrap and start

Prerequisites: Go 1.27+, Docker with Linux containers, and Compose v2 supporting
`up --wait` and bind `create_host_path: false`. No cloud service, API key, managed
database, or subscription is required. Budget roughly 1 GiB RAM plus build
capacity; caps are not performance guarantees. Local operation has no hosting
bill; a small VM is optional and provider-priced.

From the repository root, run **one** preparation command:

```powershell
# Windows / Docker Desktop
.\deploy\prepare.ps1
```

```sh
# Linux, as your regular non-root user
sh deploy/prepare.sh
```

The script builds the host CLI, creates fresh credentials under `deploy/pki`,
and writes ignored `deploy/.env`. It refuses to overwrite an existing setup.
On Linux the image runs as your numeric UID/GID, matching owner-only certificate
permissions. On Docker Desktop it uses UID/GID 65532; bind-mount permissions are
mediated by Docker Desktop. Restrict the Windows host directory ACL. Never make
keys world-readable to work around a permissions error.

```sh
docker compose --env-file deploy/.env -f deploy/compose.json config --quiet
docker compose --env-file deploy/.env -f deploy/compose.json build
docker compose --env-file deploy/.env -f deploy/compose.json up -d --wait --wait-timeout 90
docker compose --env-file deploy/.env -f deploy/compose.json ps
```

The scratch runtime contains only the static application and license artifacts,
no shell/package manager. Each voter gets its own named data volume, initialized
with the image's non-root ownership. Do not change the configured UID after
creating volumes without an explicit offline ownership migration.

The consensus network has no published ports; workers join only the API network.
API ports 8101-8103 publish to host loopback only. Root filesystems and credential
mounts are read-only. Capabilities are dropped, privilege escalation is disabled,
and process/memory/CPU/log limits are explicit. A health-only `probe` certificate
cannot submit, read job state, claim, or complete jobs. Its mTLS liveness probe
keeps followers healthy; health does **not** mean writable leadership.

Issuer keys are not mounted in any container. Move `deploy/pki/issuer` into
protected offline storage after issuance. Never mount the complete PKI tree
or share a voter data volume. The checked-in example intentionally has no
secrets; job payloads and results remain readable in authorized storage/backups.

## Submit and observe

```sh
docker compose --env-file deploy/.env -f deploy/compose.json run --rm admin cluster-client -credentials /credentials -endpoints https://node1:8100,https://node2:8100,https://node3:8100 -operation enqueue -file /job.json
docker compose --env-file deploy/.env -f deploy/compose.json run --rm admin cluster-client -credentials /credentials -endpoints https://node1:8100,https://node2:8100,https://node3:8100 -operation status -key invoice-2026-001
docker compose --env-file deploy/.env -f deploy/compose.json logs --tail 100 worker1 worker2
```

Repeat submission with the same spec/key: it returns the retained job, not
another execution. Altering the spec under the same key conflicts. To submit
other specs, use the host CLI with `deploy/pki/admin` and the loopback endpoints.
The result is a SHA-256 digest, not an external transaction.

Monitor completed/exhausted/pending/leased counts, worker JSON events, election
logs, disk space, certificate expiry, and per-node readiness. There is no
Prometheus exporter or autoscaler. `cluster-client -operation ready` against
each single endpoint identifies quorum-confirmed leadership; liveness alone
cannot. Use an external bounded readiness loop during elections.

## Failure and recovery drill

1. Stop one worker while it has a lease. After expiration, another worker can
   reclaim the job with a higher generation. Completed jobs do not re-enter the
   queue; attempts exhausted by repeated loss remain explicitly exhausted.
2. Stop the current leader using `docker compose ... stop nodeN`. The two
   survivors elect a replacement. Queries/mutations can be temporarily
   unavailable; retry stable keys. Use `stop` to avoid restart-policy masking.
3. Stop another voter. The minority must not acknowledge mutations or serve
   authoritative status. Liveness may remain healthy.
4. Start stopped voters with `docker compose ... start nodeN`. They catch up
   from the retained log/snapshot. Verify the original key/result again.
5. Run `docker compose ... down`, then `up -d --wait`. Named volumes survive
   ordinary `down`. **Do not use `down -v`: it deletes persistent state.**

Replace `...` with `--env-file deploy/.env -f deploy/compose.json`.
Forced process kills and stale completions are covered by `TestClusterProcesses`
without Docker. Graceful SIGTERM stops the API and Raft; SIGKILL cannot promise
graceful shutdown. Neither test is proof of physical power-loss durability.

## Backups, identity, and credentials

For the supported offline backup procedure: stop workers, stop all three
voters, then archive each named volume **separately** together with its original
node ID/address manifest and version metadata. Protect client certificates
separately; do not bundle issuer private keys in application backups. Copying a
live bbolt file with ordinary filesystem tools is not a supported backup.

Restore the complete stopped cluster to the same three IDs and advertised
addresses. Never clone one node's directory into a different voter or bootstrap
a replacement empty majority over surviving data. Disaster recovery from a
single survivor, membership replacement, and online backups need additional
procedures; they are not implemented by this fixed-topology reference.

Bootstrap certificates expire after 90 days. There is no hot certificate
reload, automated issuer, CRL/OCSP validation, or revocation service. For
same-CA leaf renewal, issue matching SANs/roles with your managed PKI, replace
the mounted files securely, and restart one voter at a time while retaining
quorum. Restart clients to load renewed credentials. Trust-root changes need
an explicitly planned overlap or an offline cutover of all peers/clients.
Do not rerun `cluster-pki` over existing files or delete data to rotate keys.

## Production admission checklist

This repository supplies deployment assets, not a deployed production service.
Before admitting real users: validate the containers on the target runtime;
pin reviewed image digests; separate voters across three independent failure
domains with reliable persistent disks; issue real host SANs; configure network
policy, host ACLs, secret delivery, monitoring/alerts and backup restore drills;
perform native race/load/soak and failure tests; and establish SLOs and on-call
ownership. Changing addresses currently requires a new cluster/migration plan,
not an ad-hoc edit to `identity.json`.

Hard limits remain: 4096 retained keys, 256 outstanding jobs, 64 worker
identities, three fixed voters, no key garbage collection, no lease renewal,
no arbitrary executable jobs, and no external-effect atomicity. Clock jumps can
affect expiry/liveness; generations fence stale ledger commits. Retention fills
eventually and returns an explicit capacity error: deleting keys to reclaim
space would weaken deduplication and is deliberately not automated.

FenceLab's own source license still requires the owner's choice. Dependency
attribution is in [the notices](../THIRD_PARTY_NOTICES.md); a deployment example
does not grant a project redistribution license.
