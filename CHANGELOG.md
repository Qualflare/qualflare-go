# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.1.0

Initial release.

### Added

- **A `qualflare-go` binary that reads `go test -json` and writes a Qualflare
  report.** Wrapper mode (`qualflare-go ./...`) is the documented form because
  it is the only one that sees `go test`'s exit code and its stderr; pipe and
  file modes are supported and say what they cannot detect.

- **An exit-code backstop.** On Go 1.21 and 1.23 a package that fails to compile
  produces no `fail` event at all — the JSON stream is entirely green while
  `go test` exits 1, and the compiler error goes only to stderr. When the child
  exits non-zero and nothing in the report is non-passing, a failure is
  synthesised from whatever evidence exists. On any toolchain before 1.24 this
  is the only thing standing between a broken build and a green launch.

- **A zero-dependency metadata library**: `Label`, `Link`, `Tag`, `Description`,
  `Priority`, `Parameter`, `MaskedParameter`, `Step`, `Attach`, `AttachText`.
  Every call takes a `testing.TB`, which makes metadata outside a test a compile
  error rather than a runtime rule. Nothing in the API can fail or panic a test.
  Importing it adds nothing to your module graph, and that is enforced by a test
  over the real import graph.

- **Subtests as first-class cases.** `TestX/row` is its own case with its own
  history, because table-driven tests are the Go idiom and folding a 200-row
  table into one case destroys the per-row flakiness signal.

- **The statuses Go can express but nothing reads.** A test killed by the
  deadline is `timeout`, a process that vanished without explanation is
  `aborted`, a panic that escaped the framework is `error` — rather than every
  non-terminal outcome collapsing into one bucket.

- **Retry support under `-count=N`**, the only rerun shape Go has.
  `--repeat=collapse` (default) folds occurrences into `attempts` with
  `retryCount` and `isFlaky`; `--repeat=split` emits each run as its own case
  for a flake hunt. Flakiness is never inferred across separate `go test`
  invocations — a guessed flake is worse than a missing one.

- **Diagnostics in the report.** Unparsed lines, oversized lines, unknown stream
  actions and per-case cap warnings surface as `qualflare.*` properties, so a
  launch that may be missing evidence says so rather than looking healthy. A
  clean run adds none of them.

- Configuration by flag → `QUALFLARE_*` environment → CI/git detection →
  default, matching the sibling reporters. GitHub Actions, GitLab CI, CircleCI
  and Jenkins are detected.

### Notes

- Requires Go 1.21+. Build failures appear in the JSON stream only on Go 1.24+;
  earlier toolchains rely on the exit-code backstop.
- The reporter makes no network calls. `qualflare-cli` uploads the report
  directory, which is what lets sharded jobs merge into one launch.
