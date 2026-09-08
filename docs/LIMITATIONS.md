# Known limitations

What this reporter does not do, and why. Everything here is deliberate; where a
limitation comes from Go rather than from this package, that is said plainly.

## Build failures are invisible before Go 1.24

Go only began reporting build failures as JSON events in 1.24. On 1.21 and 1.23
a package that fails to compile produces **no `fail` event at all** — the stream
contains only the packages that did build, and the compiler error goes to
stderr. Measured: one broken package alongside one passing one yields a stream
with **1 passing case and 0 failures while `go test` exits 1**.

The wrapper catches this by checking the child's exit code and synthesising a
failure when the report would otherwise be all green. **A pipe cannot**, because
it sees neither the exit code nor stderr — which is why `qualflare-go ./...` is
the documented form and `go test -json | qualflare-go` warns about it.

`GODEBUG=gotestjsonbuildtext=1` reverts a 1.24+ toolchain to the old behaviour,
with the same consequence.

## Retries exist only under `-count=N`

Go has no rerun flag and no attempt field. `retryCount`, `isFlaky` and
`attempts` are therefore populated only when the same test genuinely ran more
than once in one invocation, which means `-count=N`.

Flakiness is never inferred across separate `go test` processes — including
`gotestsum --rerun-fails`, which re-invokes the command. The reporter cannot
know whether a second stream is a rerun of the first or a different `-run`
selection, and a phantom flake is worse than a missing one. Pass the streams
explicitly with repeated `-i` if you want them treated as attempts.

## A parallel test's duration includes time it was paused

Go reports one `Elapsed` per test, measured as wall clock. For a test that calls
`t.Parallel()` that span includes the time it sat paused while other tests ran,
so a parallel test can report a duration far longer than the work it did. The
reporter passes Go's number through rather than inventing a "CPU time" it cannot
measure.

For the same reason a suite's duration is the package's own reported elapsed
time, not the sum of its cases — under parallelism the sum exceeds wall clock.

## Metadata from a goroutine that outlives its test can land on the parent

After a test completes, `t.Log` does not fail: it walks to the closest
*incomplete* parent test and attributes the line there, panicking only when the
whole chain is finished. A goroutine still running after its test returned can
therefore have its metadata recorded against the parent case.

Nothing in a library can correct this — the attribution decision happens inside
`testing` before we see it. Emit metadata from the test goroutine.

## Subtest names are Go's, ambiguity included

Go rewrites spaces in subtest names to underscores, and a subtest named `a/b` is
indistinguishable in the stream from a subtest `b` nested inside `a`. Both
arrive as the same string, and the information needed to tell them apart is not
in the output.

Names are recorded exactly as Go spells them, because that is what `-run`
accepts and what you will search for.

## Benchmarks are recorded as tests, without their measurements

A benchmark that completes is reported as a passing case and a failing one as a
failure, so `-bench` runs are not silently dropped. The timing and allocation
figures on the result line are not parsed into metrics.

## A cached package contributes no cases

`go test` prints `(cached)` and emits no test events for a package whose results
were reused, so there is nothing to report. A run where every package was cached
produces an empty report and says so; use `-count=1` to force execution.

## Packages with no test files are skipped entirely

Go emits a package-level `skip` for these. They produce no suite, rather than a
green empty one — a `./...` over a monorepo would otherwise add dozens of
meaningless entries and make the launch's skip count meaningless.

## Metadata lines are visible in the raw stream

Because metadata travels through `t.Log`, the encoded lines appear in the raw
`go test -json` output and in verbose passthrough. They are stripped from the
report's captured output, and nothing is emitted at all under plain
`go test -v`, but a user reading raw JSON will see them.

## Caps

Bounds applied per case, with anything beyond dropped and a warning logged: 300
steps, 50 parameters per step, 50 attachments, 100 labels, 20 links, 64 tags,
50 attempts.

## Not limitations of this reporter

- **No video or screenshots.** Go records neither; there is nothing to capture.
- **No `timeout`/`aborted` confusion.** Both are real wire statuses and both are
  used — a test killed by the deadline is `timeout`, a process that vanished
  without explanation is `aborted`, rather than everything collapsing into
  `error`.
- **Subtests are cases, not steps.** That is a deliberate mapping, not a
  shortfall: Go treats them as first-class (`-run TestX/sub`, independent
  elapsed, independent `t.Parallel`), and folding a table-driven test into one
  case would destroy the per-row history.
- **The library adds no dependencies.** The module requires nothing outside the
  standard library, enforced by a test over the real import graph.
