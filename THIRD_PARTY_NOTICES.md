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

## Transitive components linked into the CLI

The inventory is the union of Windows/amd64 and Linux/amd64 `go list -deps`
results for `./cmd/fencelab`, not a guess based on direct requirements alone.

| Component | Version | Why it is present | License |
|---|---|---|---|
| `github.com/boltdb/bolt` | v1.3.1 | Compatibility code in the Raft storage adapter; FenceLab opens bbolt | MIT |
| `github.com/fatih/color` | v1.19.0 | Logger formatting | MIT |
| `github.com/hashicorp/go-hclog` | v1.6.3 | Raft operational logging | MIT |
| `github.com/hashicorp/go-immutable-radix` | v1.3.1 | Metrics prefix/index support | MPL-2.0 |
| `github.com/hashicorp/go-metrics` | v0.7.0 | Upstream consensus instrumentation | MIT |
| `github.com/hashicorp/go-msgpack/v2` | v2.1.5 | Raft protocol/storage serialization | MIT |
| `github.com/hashicorp/golang-lru` | v1.0.2 | Upstream cache/index support | MPL-2.0 |
| `github.com/mattn/go-colorable` | v0.1.15 | Cross-platform terminal output | MIT |
| `github.com/mattn/go-isatty` | v0.0.24 | Terminal detection | MIT |
| `golang.org/x/sys` | v0.48.0 | Platform system calls and file locking | BSD-3-Clause + patent grant |

**Full, unmodified upstream license/notice texts are checked in under
[`licenses/`](licenses/).** The [machine-readable manifest](licenses/manifest.json)
records each component, version, original source archive URL, copied notice path,
and SHA-256. It includes the three direct modules above, these ten transitive
modules, the Go runtime/library terms, and the installed Playwright test tools.
No upstream library source was copied into FenceLab's authored packages.

The MPL-covered modules remain unmodified separate upstream works. When
distributing a binary, retain these notices and make their corresponding source
available under the MPL; exact versioned source archive URLs are included in the
manifest. This does not require relicensing unrelated FenceLab files. The Docker
runtime copies this notice and the full `licenses/` directory into the image.
Recheck the inventory and source availability when updating dependencies.

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

The Docker build stage uses `golang:1.27.1-bookworm` (the upstream Go image and
its Debian build tools); it is not the runtime base. The final image is `scratch`,
with the static CLI and notices, and includes no Debian userland. Docker,
Compose, Git, Node.js and Chromium are operator/build/test tools, not bundled
application components. Obtain their own distribution notices if redistributing
those tools. `fsevents` is an optional macOS-only Playwright graph dependency,
not installed or linked into the verified Windows/Linux application.

## Conceptual references

The [architecture reference list](docs/architecture.md#references) credits the
distributed-locking discussion and standard-library event-queue implementation
used to explain the model. The reference article is linked, not copied into this
repository.

## Project terms

No project `LICENSE` has been selected or granted. Public repository visibility
does not by itself grant reuse or redistribution rights. The upstream licenses
listed above cover their respective components, not automatically FenceLab.
