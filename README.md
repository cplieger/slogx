# slogx

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/slogx.svg)](https://pkg.go.dev/github.com/cplieger/slogx)
[![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/slogx)](https://github.com/cplieger/slogx/blob/main/go.mod)
[![Test coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/slogx/badges/coverage.json)](https://github.com/cplieger/slogx/actions/workflows/coverage.yml)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13870/badge)](https://www.bestpractices.dev/projects/13870)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/cplieger/slogx/badge)](https://scorecard.dev/viewer/?uri=github.com/cplieger/slogx)

> Standard structured-logging setup for log/slog

A tiny, standard-library-only helper that installs one standard slog handler
shape: leveled text (logfmt) or JSON, UTC-normalized timestamps, and a
`*slog.LevelVar` so the level can be set after config is read or flipped at
runtime. Plus a `LOG_LEVEL` parser that adds the long-form `warning` alias slog
lacks.

It is a thin wrapper around `log/slog`, not a logging framework and not a custom
handler. Zero dependencies beyond the standard library.

## Install

```sh
go get github.com/cplieger/slogx@latest
```

## Usage

The common case: parse `LOG_LEVEL` and install the default logger. Parse first,
install, then warn on a bad value (so the warning goes through the new handler):

```go
lvl, ok := slogx.ParseLevel(os.Getenv("LOG_LEVEL"), slog.LevelInfo)
slogx.Setup(slogx.Options{Level: lvl})
if !ok {
	// Field-name-only: a misconfigured env expansion could place a secret here.
	slog.Warn("invalid LOG_LEVEL, using default", "var", "LOG_LEVEL", "default", "info")
}
```

An app whose structured log events are the product (shipped to Loki, rendered as
dashboard columns) emits JSON to stdout:

```go
slogx.Setup(slogx.Options{Format: slogx.JSON, Output: os.Stdout})
```

Install a handler _before_ config is read (so early warnings still emit), then
set the level once it is known. The returned `*slog.LevelVar` also flips the
level at runtime for a debug toggle:

```go
lv := slogx.Setup(slogx.Options{}) // Info default, on stderr

cfg := loadConfig() // may log warnings, which emit at Info
lvl, _ := slogx.ParseLevel(cfg.LogLevel, slog.LevelInfo)
lv.Set(lvl)

// later, from a settings toggle:
func setDebug(on bool) {
	if on {
		lv.Set(slog.LevelDebug)
	} else {
		lv.Set(slog.LevelInfo)
	}
}
```

Building your own handler options? `UTCTime` is exported as the escape hatch:

```go
h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{ReplaceAttr: slogx.UTCTime})
```

## API

- `Setup(Options) *slog.LevelVar`: build a handler and install it as `slog`'s default; returns the LevelVar backing its level.
- `NewHandler(Options) (slog.Handler, *slog.LevelVar)`: the same without the `SetDefault`, for composition.
- `Options{Output, Format, Level, AddSource}`: zero value is a text handler at Info on stderr.
- `Format`: `Text` (logfmt, default) or `JSON`; any other value is a programmer error and makes `NewHandler`/`Setup` panic (`ParseFormat` only ever produces the two constants).
- `ParseLevel(raw string, def slog.Level) (slog.Level, bool)`: parse a level string (case-insensitive, `warning` alias, slog offset syntax; the alias composes with offsets, so `warning+1` parses like `warn+1`); `ok=false` on a non-empty unparseable value.
- `ParseFormat(raw string, def Format) (Format, bool)`: parse a format string (`text`/`json`, case-insensitive, trimmed); same contract as `ParseLevel`: empty returns the default with `ok=true`, a non-empty unrecognized value returns the default with `ok=false` so the caller can warn.
- `UTCTime(groups []string, a slog.Attr) slog.Attr`: the ReplaceAttr that renders timestamps in UTC.
- `capture` (subpackage `slogx/capture`): a record-capturing `slog.Handler` for tests; see [Testing](#testing).

## Testing

The `slogx/capture` subpackage records log output so a test can assert on it
without hand-rolling a buffer handler. For code that logs through
`slog.Default()`, `capture.Default(t)` swaps in a recorder and restores the
previous default on cleanup:

```go
func TestWarnsWhenFull(t *testing.T) {
	rec := capture.Default(t)

	checkDisk() // logs through slog.Default()

	if rec.Count("disk almost full") != 1 {
		t.Errorf("want one warning, got %d", rec.Count("disk almost full"))
	}
}
```

For code that takes an injected `*slog.Logger`, `capture.New()` returns a logger
plus its recorder and never touches the global default, so the test stays
parallel-safe:

```go
logger, rec := capture.New()
c := NewComponent(WithLogger(logger))
// ... exercise c, then assert on rec.Contains / Count / CountExact / Messages / Records
```

`Count` matches by substring; `CountExact` matches the whole message. Reach for
`CountExact` when a message is pinned by an external contract (a log-based
alert rule matching the exact `msg`), where a substring count would false-pass
on a superstring message. `CountLevel(level, sub)` scopes the substring count
to records at exactly one level, for escalation contracts ("one ERROR and zero
WARN of this message") that the level-blind counters cannot express.

Attribute-level assertions cover a record's top-level attributes, with
`Logger.With` derivations already folded in: `AttrValue(msgSub, key)` returns
the first match's rendered value, `HasAttr(msgSub, key, rendered)` pins an
exact rendered value, and `AttrContains(msgSub, key, sub)` matches the value
by substring. Values compare by their rendered form (`slog.Value.String()`),
so an `Int64` 7 and a string `"7"` both satisfy `"7"`; the comparison is
kind-agnostic on purpose. The empty string is a wildcard for both scoping
parameters (`msgSub` `""` matches every record, `key` `""` matches every
attribute); values nested inside groups are out of scope, so walk `Records()`
for those.

`capture` is a separate package so its `testing` import and record buffer never
reach production consumers of `slogx`; import it only from `_test.go` files.
Captured records are render-faithful: `Logger.With`/`Logger.WithGroup` nesting
is folded into each stored record, attribute values are stored resolved
(`slog.LogValuer`), and degenerate attrs follow the standard `slog.Handler`
output rules, so `rec.Records()` matches what a real handler would emit.

## Unsupported by Design

These are deliberate non-goals, not a TODO list. The library covers one
cohesive concept, installing the standard slog handler, and stays small on
purpose.

| Feature | Rationale |
| --- | --- |
| A custom `slog.Handler` implementation | `slogx` composes the standard-library `Text`/`JSON` handlers. If you need different formatting, write your own handler and use `UTCTime` in its options. |
| Secret redaction / attribute scrubbing | Keeping secrets out of logs is per-call-site discipline (log `token_set=true`, not the token). A blanket redacting ReplaceAttr gives false confidence; the library will not add one. |
| Audit-event schemas | A structured audit log (actor, action, outcome) is domain policy that belongs in the app, not in a generic logging helper. |
| `LOG_LEVEL` (or any) env-var _names_ | `ParseLevel` takes a string; the app owns which environment variable it comes from and its default. |
| Per-app attribute conventions | Base attributes (`slog.With("service", …)`), key naming, and message wording are the app's editorial choices. Call `.With` on the logger `Setup` installs. |
| A logging facade / leveled wrapper types | `slog` is the interface. `slogx` configures it and gets out of the way; it does not wrap `slog.Logger` in another type. |

## Contributing

Issues and PRs are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the
conventions and how to run the checks locally.

## Disclaimer

This project is built with care and follows security best practices, but it is
intended for personal / self-hosted use. No guarantees of fitness for production
environments. Use at your own risk.

This project was built with AI-assisted tooling using
[Claude](https://claude.com), [GPT](https://openai.com), and
[Kiro](https://kiro.dev). The human maintainer defines architecture,
supervises implementation, and makes all final decisions.

## License

GPL-3.0. See [LICENSE](LICENSE).
