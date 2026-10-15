# Project instructions

print-bridge is a Go desktop app using Fyne for its UI and Gin for a localhost
HTTP API that forwards raw print jobs to network printers over TCP.

## Before changing behavior

- Read `README.md`, `CONTRIBUTING.md`, and the relevant sections of `DOCS/`.
- Use `DOCS/03-architecture/architecture-overview.md`, `decisions.md`, and
  `security-model.md` for architecture and scope decisions.
- Use `DOCS/04-api/openapi.yaml` for the API contract,
  `DOCS/05-ux/tray-app-spec.md` for UI behavior, and
  `DOCS/06-config/config-schema.md` for configuration.
- Follow the user's requested scope and acceptance criteria. Treat existing
  design decisions as defaults; an explicit request to change them authorizes
  the corresponding work. Update affected documentation with the implementation.

## Implementation

- For Virtual Mode work, follow `DOCS/08-backlog/virtual-mode-sprint.md` in order.
  Complete each task's tests and documentation with its code, and record actual
  verification before marking it done. Planned specs do not mean the feature
  is already implemented. Use `DOCS/07-build-deploy/testing.md` for coverage.
- Keep the HTTP listener bound to `127.0.0.1` and preserve the configured CORS
  allow-list. CORS is browser access control, not general authentication.
- Preserve raw print payloads without transformation and keep explicit TCP
  connect and write timeouts.
- Keep UI wiring in `internal/desktopapp`, HTTP handling and printer transport
  in `internal/server`, persistence in `internal/config`, and logging in
  `internal/logging`.
- Preserve the single-process architecture and clean listener shutdown/restart.
- Keep changes focused. Avoid unrelated refactors and unnecessary dependencies.
- Consult documentation matching the versions in `go.mod` when library behavior
  is uncertain; use Context7 when available or official library documentation.
- Update the OpenAPI contract when API behavior changes and the configuration
  schema when settings or persistence behavior changes.
- Keep generated release artifacts out of commits and preserve license and
  fork attribution.

## Function comments

- Document exported functions and methods, and unexported functions whose
  purpose or behavior is not obvious. Start Go doc comments with the function
  or method name and briefly explain what it does.
- Include important contracts when relevant: side effects, error behavior,
  timeouts, concurrency expectations, and resource ownership or cleanup.
- Keep comments concise and maintain them when behavior changes. Simple,
  self-explanatory helpers do not need comments that merely repeat their names.
- Use inline comments to explain non-obvious reasoning or constraints, not to
  narrate each statement. Prefer clear names and straightforward code over
  excessive commentary.

## Validation

- Add or update behavior tests for API, configuration, logging, and server
  changes. Use the existing injectable TCP dialer and simulated printers so
  automated tests do not require physical hardware.
- Cover relevant failure cases, such as unreachable printers, failed writes,
  invalid requests, and listener restart failures.
- For code changes, run `make check` (format, tests, vet, and race checks) and
  `make lint` when `golangci-lint` is available. `make fmt` modifies files;
  inspect the diff for unrelated formatting changes.
- For documentation-only changes, check accuracy, paths, and the diff; running
  the Go test suite is unnecessary.
- For UI, tray, or packaging changes, verify the affected behavior on macOS
  and Windows when those environments are available.
- Report what was verified and any checks that could not run. Distinguish
  simulated printer tests from physical printing and native installer tests.
