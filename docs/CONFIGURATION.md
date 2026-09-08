# Configuration

Every option can be given as a command-line flag or an environment variable.

**Precedence, highest first: flag → `QUALFLARE_*` environment → CI/git detection
→ default.** That is the order all eight Qualflare reporters document, so one
mental model covers every package.

An empty flag or variable falls *through* rather than winning, so setting
`--environment=""` does not override a real value with nothing.

**There is deliberately no config file and no token option.** Go has no
framework config file, and inventing one would be a convention nobody asked for.
The reporter makes no network calls, so it has no credential — `qf login` holds
that.

## Options

| Flag | Environment variable | Default | Meaning |
|---|---|---|---|
| `--output-dir` | `QUALFLARE_OUTPUT_DIR` | `qualflare-results` | Directory the report files are written to. Point every shard at the same one. |
| `--environment` | `QUALFLARE_ENVIRONMENT` | `development` | Environment the run belongs to. Must already exist server-side. |
| `--language` | `QUALFLARE_LANGUAGE` | `en-US` | Report language. |
| `--framework` | `QUALFLARE_FRAMEWORK` | `golang` | Framework name. Drives the per-tool logo; change it only if you know why. |
| `--platform` | `QUALFLARE_PLATFORM` | `api` | Platform label. |
| `--milestone` | `QUALFLARE_MILESTONE` | none | Milestone sequence number. A non-numeric value is ignored rather than fatal. |
| `--branch` | `QUALFLARE_BRANCH` | CI → git → *null* | Branch name. |
| `--commit` | `QUALFLARE_COMMIT` | CI → git → *null* | Commit SHA. |
| `--run-id` | `QUALFLARE_RUN_ID` | CI build → random | Groups a launch's report files. Every shard must share one. |
| `--shard-index` | `QUALFLARE_SHARD_INDEX` | none | Which shard produced these cases. `0` is a real shard, not "unset". |
| `--enabled` | `QUALFLARE_ENABLED` | `true` | Set false to write no report at all. |
| `--repeat` | `QUALFLARE_REPEAT` | `collapse` | `collapse` folds `-count=N` runs into one case with attempts; `split` keeps them separate. |

The library reads two variables of its own:

| Variable | Meaning |
|---|---|
| `QUALFLARE_GO` | `1` forces metadata emission on, `0` off. The wrapper sets it. |
| `QUALFLARE_GO_SPILL` | Directory for payloads too large for one log line. Set by the wrapper. |

## What is detected automatically

**CI providers.** Only those whose variables are unambiguous are matched —
GitHub Actions, GitLab CI, CircleCI and Jenkins. A wrong branch name is worse
than no branch name, because the server groups history by it.

On GitHub Actions two details are deliberate. The branch comes from
`GITHUB_HEAD_REF` on a pull request, because `GITHUB_REF_NAME` there is the
merge ref (`7/merge`) and not a branch anyone recognises. And the run id
includes `GITHUB_RUN_ATTEMPT`, so re-running a workflow produces its own launch
rather than merging into the previous one.

**git**, consulted only when CI reports nothing. A detached HEAD reports the
literal `"HEAD"`, which is not a branch name and is the normal state of a CI
checkout, so it degrades to nothing rather than being recorded.

**Branch and commit stay null when nothing reports them.** The wire contract
distinguishes "not reported" from "absent", so a guess would be worse than a
null.

## Why the library emits nothing under plain `go test`

Metadata travels as encoded lines in the test log, so emitting it when nothing
is consuming the stream would put noise in your terminal for no benefit.

The gate is `go test -json`, which passes `-test.v=test2json` — measured on Go
1.21, 1.23, 1.25 and 1.26, so it holds at this module's floor. Plain
`go test -v` sets `"true"` and is not enough. `QUALFLARE_GO=1` overrides the
gate, and the wrapper sets it for you.
