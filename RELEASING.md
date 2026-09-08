# Releasing

## Cutting a release

1. Make sure `main` is green — CI and E2E both.
2. Tag it. The version comes from the tag; there is no version file to keep in
   step.

   ```bash
   git tag -a v0.2.0 -m "v0.2.0" && git push origin v0.2.0
   ```

3. `release.yml` runs on `v*`. Before goreleaser touches anything it re-runs the
   whole gate — `gofmt`, `go vet`, `go test -race`, the zero-dependency check —
   at the exact bytes about to be published, then asserts the **built binary
   reports the tag**. That last check is the Go equivalent of the tag-vs-manifest
   check the npm and PyPI reporters run, and it has already caught one broken
   build before it shipped.
4. Verify at the sources, never at the green checkmark:

   ```bash
   go install github.com/Qualflare/qualflare-go/cmd/qualflare-go@vX.Y.Z
   gh release view vX.Y.Z
   gh api repos/Qualflare/homebrew-tap/commits --jq '.[0].commit.message'
   ```

   A green workflow is not evidence of a publish. `@qualflare/cli` once reported
   `OK Test results collected successfully` for eleven consecutive runs while
   nothing reached the server.

## What the release publishes

Archives for linux, macOS and windows on amd64 and arm64; `checksums.txt` with a
keyless cosign signature and certificate; a CycloneDX SBOM per archive; and a
Homebrew formula pushed to `qualflare/homebrew-tap`.

The formula requires `HOMEBREW_TAP_TOKEN` on this repository. Without it
goreleaser's `skip_upload` guard skips that step rather than failing the release
— which is what happened on v0.1.0, and why v0.1.1 exists.

`go install` and `pkg.go.dev` need nothing from us; the module proxy picks up
the tag on its own.

## After the release

This section exists because none of the sibling reporters has one, and the
result was that eight packages shipped with no discovery work at all.

- **Nothing to do for pkg.go.dev or the module proxy.** They index the tag
  automatically. Confirm with
  `curl -s https://proxy.golang.org/github.com/!qualflare/qualflare-go/@v/list`.
- **Check the Codecov page still resolves.** It is the coverage evidence the
  awesome-go submission depends on.
- **Listings are tracked in the plan, not here**, because they are dated:
  - `nikolaydubina/go-recipes` — no eligibility bar; submittable now.
  - `avelino/awesome-go` — requires five months of history from the first
    commit (2026-09-08), so **not before 2027-02-10**. Their CI treats maturity
    as a warning rather than a hard failure, but a `needs-maturity` label on a
    young repo is what gets a PR closed with "resubmit later".
  - `atinfo/awesome-test-automation` — PR #572 is open and covers the service.
    Do not add a second PR; that queue has not moved in months.
- **Every submission discloses the affiliation.** Qualflare is a commercial
  product and this is its client. Lists tolerate vendor tools; they do not
  tolerate finding out later.

## Notes

- A prerelease tag never publishes the Homebrew formula — goreleaser's
  `skip_upload` guard checks `not .Prerelease`, so an rc cannot overwrite the
  formula `brew install` resolves.
- Do **not** add a Go Report Card badge. The service was sunset and the report
  URL now serves a thank-you page. The URL still belongs in the awesome-go PR
  body, because their checker only requires it to resolve.
