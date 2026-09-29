# Third-party components and references

FenceLab's original laboratories use Go's standard library and browser-native
HTML, CSS, JavaScript, and SVG. The separate replicated execution mode also uses
the following pinned upstream implementations. There is no bundled frontend
framework, external font, icon pack, or hosted API.

## Replicated execution dependencies

| Component | Role | Upstream terms |
|---|---|---|
| `github.com/hashicorp/raft` v1.8.0 | Consensus, elections, replication and snapshots | [MPL-2.0](https://github.com/hashicorp/raft/blob/v1.8.0/LICENSE) |
| `github.com/hashicorp/raft-boltdb/v2` v2.4.2 | Durable Raft log/stable-store adapter | [MPL-2.0](https://github.com/hashicorp/raft-boltdb/blob/v2.4.2/LICENSE) |
| `go.etcd.io/bbolt` v1.4.1 | Synchronous embedded database | [MIT](https://github.com/etcd-io/bbolt/blob/v1.4.1/LICENSE) |

`go.mod` and `go.sum` pin the complete resolved Go dependency graph. Upstream
implementations are imported without modifying their source. Their licenses
apply to their components; they do not automatically license FenceLab's own code.

## Build and test dependencies

| Component | Role | Upstream terms |
|---|---|---|
| Go toolchain and standard library | Build/runtime | [BSD-style LICENSE](https://go.dev/LICENSE) and [PATENTS](https://go.dev/PATENTS) |
| `@playwright/test`, `playwright`, `playwright-core` 1.63.0 | Browser testing only | [Microsoft Playwright](https://github.com/microsoft/playwright), Apache-2.0; each installed package contains `LICENSE` and `NOTICE` |
| Chromium installed by Playwright | External test browser, not embedded in FenceLab | Chromium and its bundled components retain their own notices |

`package-lock.json` pins the browser-test dependency graph, including any optional
platform-specific packages. Test tools are not shipped inside the application
binary. Preserve the relevant upstream notices when distributing those tools
or compiled artifacts; this table does not replace their full license texts.

## Conceptual references

The [architecture reference list](docs/architecture.md#references) credits the
distributed-locking discussion and standard-library event-queue implementation
used to explain the model. The reference article is linked, not copied into this
repository.

## Project terms

No project `LICENSE` has been selected or granted. Public repository visibility
does not by itself grant reuse or redistribution rights. The upstream licenses
listed above cover their respective components, not automatically FenceLab.
