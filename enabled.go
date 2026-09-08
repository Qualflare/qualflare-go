package qualflare

import (
	"flag"
	"os"
	"sync"
)

// Enabled reports whether anything is listening.
//
// Metadata rides t.Log, so emitting when no reporter is consuming the stream
// would put encoded noise in a developer's `go test -v` output for no benefit.
// The gate is `go test -json`, which passes -test.v=test2json -- measured on Go
// 1.21, 1.23, 1.25 and 1.26, so it holds at this module's floor. Plain
// `go test -v` sets "true" and is not enough.
//
// QUALFLARE_GO=1 forces emission on (the wrapper sets it, which is also what
// makes it work on a toolchain whose -json does not imply test2json), and
// QUALFLARE_GO=0 forces it off.
func Enabled() bool {
	gateOnce.Do(func() { gate = resolveGate() })
	return gate
}

var (
	gateOnce sync.Once
	gate     bool
)

func resolveGate() bool {
	switch os.Getenv(EnvEnable) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	// Resolved lazily rather than in init(): testing.Init registers this flag,
	// and at package-init time it does not exist yet.
	f := flag.Lookup("test.v")
	if f == nil {
		return false
	}
	return f.Value.String() == "test2json"
}

// Environment variables the library reads.
const (
	EnvEnable = "QUALFLARE_GO"
	EnvSpill  = "QUALFLARE_GO_SPILL"
)
