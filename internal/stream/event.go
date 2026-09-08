// Package stream decodes a `go test -json` event stream.
//
// Two properties matter more than anything else here, and both are lessons from
// qualflare-cli's Go parser rather than theory:
//
//   - A single oversized line must never cost the whole report. That parser
//     uses bufio.Scanner with a 1 MB cap; one long log line returns
//     bufio.ErrTooLong, ends the scan, and the entire run is lost. Here an
//     oversized line is truncated and counted, and decoding continues.
//
//   - A line that is not JSON must not vanish. `go help test` states plainly
//     that non-JSON text on stderr is expected and "consumers should be robust
//     to this" -- it is where a pre-1.24 toolchain reports build failures. That
//     parser silently continues; here it is retained and counted, so the
//     caller can surface it rather than upload a launch that omits the only
//     evidence.
package stream

import "time"

// Event is one decoded record.
//
// It covers both event shapes the toolchain emits. Test events key on Package;
// build events (Go 1.24+) key on ImportPath and carry NO Package at all -- and
// the ImportPath is the test BINARY path ("example.com/m/foo.test"), which
// deliberately does not equal the package ("example.com/m/foo"). Joining the two
// by Package equality matches nothing, while appearing to work in any single-
// package test. Measured on Go 1.26.3.
type Event struct {
	Time        *time.Time `json:"Time,omitempty"`
	Action      string     `json:"Action"`
	Package     string     `json:"Package,omitempty"`
	Test        string     `json:"Test,omitempty"`
	Elapsed     *float64   `json:"Elapsed,omitempty"`
	Output      string     `json:"Output,omitempty"`
	FailedBuild string     `json:"FailedBuild,omitempty"`

	// Go 1.25 t.Attr
	Key   string `json:"Key,omitempty"`
	Value string `json:"Value,omitempty"`
	// Go 1.26 t.ArtifactDir
	Path string `json:"Path,omitempty"`
	// Build events only.
	ImportPath string `json:"ImportPath,omitempty"`
}

// Actions the toolchain is known to emit. Anything else is counted and ignored:
// an unknown action must never be treated as terminal, or a future Go release
// could silently change a test's status.
var knownActions = map[string]bool{
	"start": true, "run": true, "pause": true, "cont": true,
	"pass": true, "fail": true, "skip": true, "output": true, "bench": true,
	"attr": true, "artifacts": true,
	"build-output": true, "build-fail": true,
}

// IsTerminal reports whether the action decides a test's or package's outcome.
func IsTerminal(action string) bool {
	switch action {
	case "pass", "fail", "skip", "bench":
		return true
	}
	return false
}

// ElapsedNanos converts the Elapsed field (seconds) to integer nanoseconds.
// Absent Elapsed is a legitimate zero -- never use it as a proxy for "no
// terminal event", which is what the explicit state machine is for.
func (e Event) ElapsedNanos() int64 {
	if e.Elapsed == nil {
		return 0
	}
	return int64(*e.Elapsed*1e9 + 0.5)
}
