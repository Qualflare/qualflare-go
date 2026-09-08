# Metadata API

```go
import "github.com/Qualflare/qualflare-go"
```

Every call takes a `testing.TB` as its first argument, works with `*testing.T`,
`*testing.B` and `*testing.F` alike, and returns nothing.

## How it reaches the report

A Go test body has no channel to a reporter, so metadata rides `t.Log` as a
single encoded line and is read back out of the `output` events in the
`go test -json` stream. The encoding is base64url inside a delimited envelope
with a checksum, which is what lets it survive being embedded in a log line and
keeps it distinguishable from your own output.

Three consequences worth knowing:

- **Nothing is emitted unless a reporter is listening** (see
  [CONFIGURATION.md](./CONFIGURATION.md)), so `go test -v` stays clean.
- **The explicit `tb` decides attribution.** Metadata passed a subtest's `t`
  belongs to that subtest, never to its parent. There is no inference.
- **Metadata outside a test is a compile error**, not a runtime rule — there is
  no `tb` to pass. `TestMain` receives an `*testing.M`, which is not a
  `testing.TB`, and `init()` has no test at all. Run-level information belongs
  in configuration.

## Nothing here can fail your test

No function returns an error and none panics. The single call into `t.Log` is
guarded, because `t.Log` genuinely panics with *"Log in goroutine after Test has
completed"* once a test and all its parents are done — which a stray goroutine
can trigger. A dropped metadata line is the correct outcome; a crashed suite
never is.

## `Label(tb, name, value)`

Allure-style name/value metadata: `epic`, `feature`, `story`, `owner`, and
anything else you group by.

```go
qualflare.Label(t, "feature", "checkout")
```

## `Link(tb, url, linkType, name)`

An external link. `linkType` is `qualflare.LinkIssue`, `qualflare.LinkTMS` or
`qualflare.LinkCustom`; empty defaults to custom. An unrecognised type is passed
through and rejected server-side rather than silently rewritten.

```go
qualflare.Link(t, "https://example.com/issue/42", qualflare.LinkIssue, "QF-42")
```

## `Tag(tb, tags...)`

Free-form tags. Each is clipped to 255 characters, and at most 64 reach a case.

```go
qualflare.Tag(t, "smoke", "checkout")
```

## `Description(tb, text)` and `Priority(tb, level)`

Prose describing what the test covers, and its importance —
`PriorityLow`, `PriorityMedium`, `PriorityHigh`, `PriorityCritical`. An
unrecognised priority is dropped rather than sent; the server normalises it away
anyway.

## `Parameter(tb, name, value)`

A case- or step-level parameter. A parameter emitted while a `Step` is open
belongs to that step; otherwise it belongs to the case.

```go
qualflare.Step(t, "add to cart", func() {
    qualflare.Parameter(t, "sku", "widget")   // belongs to the step
})
qualflare.Parameter(t, "plan", "pro")         // belongs to the case
```

## `MaskedParameter(tb, name)`

Records that a parameter exists without recording its value.

**It deliberately takes no value.** The wire contract treats `masked` as a
display hint and the server does not redact, so withholding the value at the
source is the only thing that actually keeps a secret out of the report — and a
signature that cannot accept one cannot leak one.

```go
qualflare.MaskedParameter(t, "token")
```

## `Step(tb, name, fn)`

Records a named step around `fn`. Steps nest lexically:

```go
qualflare.Step(t, "checkout", func() {
    qualflare.Step(t, "add to cart", func() { ... })
    qualflare.Step(t, "pay", func() { ... })
})
```

Closure form only, and that is a correctness decision rather than a taste one. A
begin/end pair would leak an unclosed step on `t.Fatal`, which calls
`runtime.Goexit` — and Goexit *does* run deferred functions, so a `defer` closes
the step where an explicit `End()` would simply never be reached.

A step is marked failed when the test transitions from passing to failing inside
it. The transition matters: using `t.Failed()` absolutely would mark every step
after the first failure as failed, however green it was.

A panic inside a step is recorded as the step's error and then **re-raised
unchanged**. Nothing here alters control flow — swallowing a panic would turn a
failing test green.

At most 300 steps are recorded per test. Beyond that, further steps are dropped
with a warning, and the ones already open keep their own timing.

## Caps

Applied per case, with anything beyond dropped and a warning logged: 300 steps,
50 parameters per step, 50 attachments, 100 labels, 20 links, 64 tags.
