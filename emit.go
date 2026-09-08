package qualflare

import (
	"fmt"
	"os"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/sentinel"
)

// logger is the two-method view of testing.TB this package actually needs.
// testing.TB cannot be implemented outside the testing package, so depending on
// the narrow interface is what makes the emitter unit-testable with a fake.
type logger interface {
	Log(args ...any)
	Helper()
}

// emit writes one message into the test's log.
//
// NOTHING HERE MAY FAIL OR PANIC A USER'S TEST. That is not a nicety: t.Log
// panics with "Log in goroutine after Test has completed" when the test and all
// its parents are done, and that panic is raised synchronously on the caller's
// goroutine -- where this recover catches it. A dropped metadata line is the
// correct outcome; a crashed suite never is.
func emit(tb testing.TB, m sentinel.Message) {
	if tb == nil || !Enabled() {
		return
	}
	emitTo(tb, m)
}

func emitTo(l logger, m sentinel.Message) {
	defer func() { _ = recover() }()
	l.Helper()

	line, err := sentinel.Encode(m)
	if err != nil {
		// Oversized payloads are spilled by the caller; anything else that
		// cannot be encoded is reported to the user's own stderr rather than
		// into the test log, where it would pollute the case's error text.
		fmt.Fprintf(os.Stderr, "[qualflare-go] dropped a %s message: %v\n", m.Kind, err)
		return
	}
	l.Log(line)
}
