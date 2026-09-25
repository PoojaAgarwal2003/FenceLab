# Third-party components and references

FenceLab's application uses Go's standard library and browser-native HTML,
CSS, JavaScript, and SVG. It has no third-party Go module dependency, bundled
frontend framework, external font, icon pack, or hosted API.

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
