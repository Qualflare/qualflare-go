# qualflare-go

[![Go Reference](https://pkg.go.dev/badge/github.com/Qualflare/qualflare-go.svg)](https://pkg.go.dev/github.com/Qualflare/qualflare-go)
[![CI](https://github.com/Qualflare/qualflare-go/actions/workflows/ci.yml/badge.svg)](https://github.com/Qualflare/qualflare-go/actions/workflows/ci.yml)
[![Qualflare](https://api.qualflare.com/p/qualflare-go/badge.svg)](https://reports.qualflare.com/p/qualflare-go/launches)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

A native Go test reporter for [Qualflare](https://qualflare.com) — captures results
directly from `go test`: statuses Go's JSON stream can express but nobody reads,
subtests as first-class cases, nested steps, attachments, and author-facing
metadata (labels, links, tags, priority, custom parameters).

> **Unreleased.** Everything below works from a source checkout, but there is
> no tagged release yet, so `go install ...@latest` has nothing to fetch. Build
> it with `go build ./cmd/qualflare-go` in the meantime.

Without it, Go results reach Qualflare through `qualflare-cli`'s `go test -json`
parser, which records a status, a duration and a name — no steps, no
attachments, no metadata, no flakiness, and every non-terminal outcome flattened
into `error`.

The reporter makes **no network calls**. It writes a report directory, and
[`qualflare-cli`](https://github.com/Qualflare/qualflare-cli) uploads it — which
is what lets any number of sharded CI jobs merge into a single Launch.

## Why this one is not a plugin

Every other Qualflare reporter hooks into its framework. Go has no reporter
plugin API and no per-test hook, and `TestMain` receives only an exit code from
`m.Run()` — so a library alone can never see every test, only the ones that
opted in. Complete results have to come from `go test -json`.

That splits the package in two, and the split explains most of what follows:

- **The binary** consumes the event stream and writes the report. It is the only
  part that can see every test.
- **The library** you import in your tests adds metadata to them. It cannot
  report a result, and it never tries to.

## Install

```bash
go install github.com/Qualflare/qualflare-go/cmd/qualflare-go@latest
```

For the metadata API, add the module to your own project:

```bash
go get github.com/Qualflare/qualflare-go
```

Requires Go **1.21+**. The module has **zero external dependencies**, so
importing it adds nothing to your `go.mod` graph — that is enforced by a test,
not a promise.

## Quickstart

```bash
qualflare-go ./...
npm install -g @qualflare/cli
qf login my-project "$QUALFLARE_TOKEN" --force
qf my-project collect ./qualflare-results
```

`qualflare-go ./...` is shorthand for `qualflare-go -- go test ./...`; it adds
`-json` for you. Pass any flags `go test` accepts:

```bash
qualflare-go -- go test -race -timeout 5m ./...
```

**Prefer the wrapper over a pipe.** `go test -json ./... | qualflare-go` works,
but a pipe cannot see two things that matter:

- **The exit code.** Without `set -o pipefail` a shell reports the pipe's
  success, not `go test`'s.
- **stderr.** On Go 1.21 and 1.23 a *build failure produces no `fail` event at
  all* — the JSON stream is entirely green while `go test` exits 1, and the
  compiler error goes only to stderr. Measured, not theorised.

The wrapper sees both, so it can fail loudly where a pipe would upload a green
launch for a broken build. In pipe mode the reporter says so rather than
pretending.

## Subtests are cases

`TestUsers/rejects_a_blank_email` becomes its own case, not a step inside
`TestUsers`. Table-driven tests are the Go idiom, and folding a 200-row table
into one case would destroy exactly the per-row history and flakiness signal
that makes the data worth having. Steps stay reserved for what you mark
explicitly.

## Parallel runs and sharding

Point every shard at the **same** `--output-dir` and collect once at the end.
Each invocation writes its own uniquely-named file, so shards never overwrite
each other, and `qf collect` merges every file in the directory into one Launch.

Shards must share a run id. In CI it is derived from the build automatically, so
they agree without coordinating; otherwise pass `--run-id`. Use `--shard-index`
to attribute cases to a worker — Go has no equivalent of `PYTEST_XDIST_WORKER`.

## Enriching your tests

```go
import "github.com/Qualflare/qualflare-go"

func TestCheckout(t *testing.T) {
    qualflare.Label(t, "feature", "checkout")
    qualflare.Tag(t, "smoke")
    qualflare.Link(t, "https://example.com/issue/42", qualflare.LinkIssue, "QF-42")
    qualflare.Priority(t, qualflare.PriorityHigh)

    qualflare.Step(t, "add to cart", func() {
        qualflare.Parameter(t, "sku", "widget")
        qualflare.MaskedParameter(t, "token")
    })
}
```

Every call takes a `testing.TB` first. That is not ceremony: Go has no
goroutine-local storage, so the explicit `t` is the only reliable handle on
which test is running — and it makes metadata outside a test a **compile error**
rather than a runtime rule, because there is no `t` to pass.

Nothing in the API can fail your test. No function returns an error, none
panics, and calls are inert unless a reporter is actually listening — so
`go test -v` stays clean.

`MaskedParameter` takes no value at all. `masked` is a display hint the server
does not act on, so withholding the value here is the only thing that actually
keeps a secret out of the report; a signature that cannot accept one cannot
leak one.

Full reference in [`docs/METADATA-API.md`](./docs/METADATA-API.md).

## Configuration

Precedence, highest first: **flag → `QUALFLARE_*` environment → CI/git detection
→ default**. Every option and variable is in
[`docs/CONFIGURATION.md`](./docs/CONFIGURATION.md).

There is deliberately no config file and no token option. The reporter makes no
network calls, so it has no credential; `qf login` holds it.

## Test reports

This reporter is tested with itself. `e2e/` is a Go suite covering this
package's own behaviour — the metadata API, nested steps, subtests as cases,
attachments and the step-cap regression — run by this reporter and uploaded to
Qualflare on every merge to `main` by the **published** `qualflare-cli`. The
results below are that suite's, reported through the code this README documents:

[![Qualflare](https://api.qualflare.com/p/qualflare-go/banner.svg)](https://reports.qualflare.com/p/qualflare-go/launches)

Every case there is meant to pass, so a red run is a real regression rather than
a fixture failing on purpose. The deliberately awkward cases — panics, timeouts,
`os.Exit`, a package that does not compile — live in
`test/integration/fixtures/`, which is never uploaded.

## Known limitations

- **Go has no retry concept.** `retryCount`, `isFlaky` and `attempts` are
  populated only under `-count=N`, where the same test genuinely runs more than
  once. Flakiness is never inferred across separate `go test` invocations —
  a guessed flake is worse than a missing one.
- **Build failures are invisible to the stream before Go 1.24.** They are caught
  by the wrapper's exit-code check instead, which is why pipe mode is the weaker
  mode.
- **Metadata from a stray goroutine can be misattributed.** After a test
  completes, `t.Log` reattributes to the closest incomplete parent rather than
  failing, and that is not something a library can correct.

Full details in [`docs/LIMITATIONS.md`](./docs/LIMITATIONS.md).

## Development

```bash
go test ./...                  # unit
go test -race ./...
go vet ./...
go build -o /tmp/qualflare-go ./cmd/qualflare-go

# the dogfood, then the check that runs before any upload
cd e2e && /tmp/qualflare-go --output-dir e2e-results -- go test -count=1 ./...
QUALFLARE_OUTPUT_DIR=e2e-results go run ./verify
```

`test/integration/fixtures/awkward` is a separate module of deliberately
difficult packages — a parent that fails while its subtests pass, a panicking
subtest, a goroutine panic, a timeout, `os.Exit(3)`, a package with no test
files, eight parallel tests logging 100 KB each. The unit tests run against
streams captured from really running it, so a diff in a capture is itself the
alarm that a Go release changed the output shape.

## License

Apache-2.0 — see [LICENSE](./LICENSE).
