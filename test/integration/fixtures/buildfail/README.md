# buildfail

A module with one package that does not compile, alongside one that does.

It lives in its own module, separate from `awkward/`, precisely because it
cannot build: the CI job that vets the fixtures would fail on it, and
`go build ./...` in the parent module must never try.

It exists for one measured behaviour. On Go 1.21 and 1.23 a package that fails
to compile produces **no `fail` event at all** — the JSON stream carries only
the packages that did build, and the compiler error goes to stderr. The stream
is entirely green while `go test` exits 1. That is what the reporter's
exit-code backstop is for, and this is the only fixture that exercises it.
