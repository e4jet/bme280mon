# Guidelines

These guidelines are written to ensure code consistency, maintainability, and supportability for this codebase.

The terms described in [rfc2119](https://datatracker.ietf.org/doc/html/rfc2119) are leveraged in this set of rules.

- **MUST** rules are enforced by CI/review
- **MUST NOT** rules are enforced by CI/review
- **SHOULD** rules are strong recommendations
- **MAY** rules are allowed without extra approval

## Continuous Integration

- **MUST** lint, vet, test (`-race`), and build on every PR

## Branching

- **MUST** use branches other than main for development
- **MUST** use pull request to merge/promote code to main

## Review

- **SHOULD** review code for proper functionality (does it do what it is supposed to do), idiomatic go, bugs and security issues
- **MUST** be concise and appropriate

## Design

- **MUST** ask clarifying questions for ambiguous requirements
- **MUST** draft and confirm an approach (data flow, failure modes) before writing code
- **SHOULD** list pros/cons when >2 approaches exist
- **SHOULD** define testing strategy (unit/integration) and observability

## Security

- **MUST** validate inputs
- **MUST** set explicit I/O timeouts
- **MUST** prefer TLS for secure communication
- **MUST NOT** use SSL (deprecated & insecure) for secure communication
- **MAY** Use ssh for secure communication with nodes
- **MUST NOT** log secrets, passwords, or credentials
- **MUST** use TLS 1.2 minimum with FIPS-approved cipher suites when configuring `tls.Config`
- **SHOULD** limit filesystem/network access by default
- **SHOULD** adhere to the principle of least privilege
- **MAY** add fuzz tests for untrusted inputs

## Modules

- **SHOULD** prefer go stdlib
- **MUST** track transitive size and licenses when introducing dependencies
  - **SHOULD** use `go mod graph` and `go mod why` to understand dependencies (most should report `main module does not need package`)
- **MUST** only introduce dependencies when there is a clear payoff
- **MUST NOT** use GNU General Public License or Lesser General Public License
- **MUST NOT** use any license that, by its terms, requires or conditions the use or distribution of such code on the disclosure, licensing, or distribution of any source code
- **MUST** use `govulncheck ./...` periodically to check for vulnerabilities (`go install golang.org/x/vuln/cmd/govulncheck@latest`)

## Style

- **MUST** enforce `gofmt`
- **MUST** use structs for the input into function receiving more than 3 arguments. Contexts should not get placed into the in the input struct
- **SHOULD** prefer small, focused functions
- **SHOULD** write clear and concise comments for exported functions and structs
- **SHOULD** declare function input structs before the function consuming them

## Errors

- **MUST** wrap the most relevant error with `%w` and context: `fmt.Errorf("open %s: %w", p, err)`
- **MUST** use `errors.Is`/`errors.As` for control flow. String matching must not be used
- **SHOULD** define sentinel errors in the package

## Concurrency

- **MUST** use **sender** to close channels
- **MUST NOT** receivers must not close channels
- **MUST** tie goroutine lifetime to a `context.Context`
- **MUST** protect shared state with `sync.Mutex`/`atomic`

## Contexts

- **MUST** `ctx context.Context` be the first parameter to a function
- **MUST NOT** store ctx in structs
- **MUST** propagate non‑nil `ctx`
- **SHOULD** use `context.Context` for request-scoped values and cancellation

## Testing

- **SHOULD** use table‑driven tests. These must be deterministic and hermetic by default
- **MUST** run `-race` in CI; add `t.Cleanup` for teardown
- **SHOULD** mark safe tests with `t.Parallel()`
- **MUST** use build tags for unit tests: `//go:build unit` and name the file `xxx_test.go`
- **MUST** use build tags for integration tests: `//go:build integration` and name the file `xxx_integration_test.go`

## Logging

- **MUST** log version string as part of startup message
- **MUST** use structured logging (`slog`) with levels and consistent fields
- **SHOULD** correlate logs/metrics/traces via request IDs from context

## File I/O

- **MUST** write files atomically: write to a temporary file in the same directory as the target, then rename it into place (`os.Rename`). This ensures a crash during the write leaves the original file intact and the rename is a single syscall on the same filesystem

## Performance

- **MUST** profile before optimizing
- **SHOULD** avoid reflection in code paths where speed is critical
- **SHOULD** avoid allocations on hot paths
- **SHOULD** reuse buffers/connections with care

## CMD

- **MUST** config via config file
- **MUST** treat config as immutable after init
- **SHOULD** provide sane defaults and clear docs
