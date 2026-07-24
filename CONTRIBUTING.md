# Contributing to slogx

Notes on the surface, the design contract, and the local workflow. Most of the
guidance is about keeping the library a thin `log/slog` helper rather than
letting it grow into a logging framework.

## A helper, not a framework

`slogx` is a standard-library-only package (no runtime or test dependencies)
that configures `log/slog` the standard way. It composes the stdlib `Text`/`JSON`
handlers; it does not implement its own `slog.Handler`, wrap `slog.Logger`, or
introduce leveled-logger types. The whole surface is five ideas:

- **`Setup` / `NewHandler`**: build the standard handler (text or JSON, UTC
  timestamps, leveled by a `*slog.LevelVar`); `Setup` also installs it as the
  default. The returned `LevelVar` is the one primitive that covers both
  install-before-config and runtime level toggling, so there is no separate
  "set level" or "toggle debug" function.
- **`Options`**: zero-value-usable (text / Info / stderr). New fields must keep
  the zero value meaning "the standard default".
- **`ParseLevel`**: the `LOG_LEVEL` superset parser (adds the `warning` alias,
  keeps slog's offset syntax, reports recognized-or-not so the caller warns
  _after_ the handler is installed).
- **`ParseFormat`**: the log-format parser (`text` or `json`) with the same
  trim, case-folding, empty-default, and recognized-or-not contract as
  `ParseLevel`.
- **`UTCTime`**: the exported ReplaceAttr, so a consumer building its own
  handler options can still get UTC timestamps.

Plus one test-support subpackage:

- **`slogx/capture`**: a record-capturing `slog.Handler` for tests (`New` to
  inject a logger, `Default(t)` to swap the global default with auto-restore).
  It lives in its own package so its `testing` import never reaches production
  consumers of `slogx`. It honors the full `slog.Handler` derivation contract:
  `WithAttrs`/`WithGroup` return handles over the shared record buffer, so
  captured records carry inherited attrs/groups with real-handler nesting.
  Keep it minimal: assertion sugar (`Contains`/`Count`/`CountExact`/
  `Messages`) plus `Records()` as the escape hatch.

## Unsupported by design: a binding contract

The "[Unsupported by design](README.md#unsupported-by-design)" table in
`README.md` lists deliberate non-features: a custom handler, secret redaction,
audit schemas, env-var names, per-app attribute conventions, a logging facade. A
PR adding one will be declined regardless of quality; each belongs in the
consuming app or is a different library. If you think a non-goal should change,
open an issue first.

Scope discipline runs the other way too: resist folding a single app's policy
into the helper. `ParseLevel` gained the `warning` alias because several apps
independently hand-rolled it; it will not grow a knob per caller. A new `Options`
field earns its place only when a real app needs it, never for symmetry.

## Local development

The module targets the Go version pinned in `go.mod` (use that toolchain or
newer; `GOTOOLCHAIN=auto` fetches it).

```sh
go build ./...
go test ./...
go test -race ./...
```

Run with `-race` before pushing: the handler tests write through `slog` and the
`Setup` test mutates and restores the global default.

### Linting and formatting

Lint config is `.golangci.yaml` (golangci-lint v2), synced from `cplieger/ci`.
`golangci-lint run` reports unformatted files as issues, so format before
pushing. `sloglint` is kv-only: log with key/value pairs.

```sh
golangci-lint run
golangci-lint fmt
```

### Fuzzing

`FuzzParseLevel` (in `level_fuzz_test.go`) is the untrusted-input boundary: a
`LOG_LEVEL` value is operator-supplied. It asserts the parser never panics,
never returns a non-default level on rejection, and always maps a blank value to
the default. Run it with a time budget and add a seed for any new parsing edge:

```sh
go test -run='^$' -fuzz=FuzzParseLevel -fuzztime=30s .
```

### Mutation testing

`.gremlins.yaml` configures [Gremlins](https://gremlins.dev) (synced from
`cplieger/ci`; change it upstream):

```sh
gremlins unleash .
```

## Test layout

Tests live beside the code, split by intent; match the right file when adding
cases:

- `level_test.go`: the `ParseLevel` table (aliases, offsets, defaults,
  rejection); `level_fuzz_test.go`: `FuzzParseLevel`.
- `format_test.go`: the `ParseFormat` table (same trim/lower/empty/ok contract
  as `ParseLevel`); `format_fuzz_test.go`: `FuzzParseFormat`.
- `attr_test.go`: `UTCTime` (rewrites a top-level time to UTC, leaves non-time
  and grouped attrs alone).
- `handler_test.go`: `NewHandler` text/JSON output, UTC normalization, level
  filtering via the live `LevelVar`, `AddSource`, the invalid-`Format` panic,
  and `Setup` installing the default (non-parallel; saves and restores
  `slog.Default`).
- `example_test.go`: runnable `Example` functions that double as docs; keep
  their `// Output:` blocks correct. `bench_test.go`: allocation benchmarks.
- `capture/capture_test.go`: the `slogx/capture` subpackage (record capture,
  inject vs global default-swap, snapshot copy, concurrency, and the
  `WithAttrs`/`WithGroup` derivation: inheritance nesting, empty-group elision,
  receiver-return contract edges, sibling non-aliasing);
  `capture/example_test.go` its runnable example.

## Commits and PRs

Branch from `main`, keep changes focused with tests, and open a PR. This account
uses [Conventional Commits](https://www.conventionalcommits.org/) parsed by
git-cliff (`cliff.toml`) to build release notes, so the commit type drives the
version bump: `feat:`, `fix:`, `sec:`, and `chore:`/`docs:`/`refactor:`/`test:`
(no release). Write the subject as the changelog line a consumer would read.

## Conduct & security

By participating you agree to the org-wide
[Code of Conduct](https://github.com/cplieger/.github/blob/main/CODE_OF_CONDUCT.md).
Report security issues through the
[security policy](https://github.com/cplieger/.github/blob/main/SECURITY.md),
never in a public issue.
