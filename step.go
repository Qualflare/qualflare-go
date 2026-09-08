package qualflare

import (
	"fmt"
	"testing"
	"time"

	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
	"github.com/Qualflare/qualflare-go/internal/textutil"
)

// Step records a named step around fn.
//
// Closure form only, and that is a correctness decision rather than a taste
// one. A begin/end pair would leak an unclosed step on t.Fatal, which calls
// runtime.Goexit -- and Goexit DOES run deferred functions, so a defer closes
// the step correctly where an explicit End() would simply never be reached.
//
// The user's panic is re-raised unchanged after the step is recorded. Nothing
// here alters control flow: a reporter that swallowed a panic would turn a
// failing test green, which is the worst thing it could possibly do.
func Step(tb testing.TB, name string, fn func()) {
	if tb == nil {
		return
	}
	step(tb, name, fn)
}

// stepper is the surface Step actually needs. Depending on it rather than on
// testing.TB is what makes the panic and t.Fatal paths testable: testing.TB
// carries an unexported method and cannot be implemented outside the testing
// package, so a fake TB is impossible.
type stepper interface {
	logger
	Failed() bool
}

func step(tb stepper, name string, fn func()) {
	if !Enabled() {
		fn()
		return
	}

	// Captured BEFORE the body so the step's status reflects what the STEP did.
	// Using tb.Failed() absolutely would mark every step after the first
	// failure as failed, however green the step itself was.
	failedBefore := tb.Failed()
	started := time.Now()

	emitTo(tb, sentinel.Message{Kind: sentinel.KindStepStart, Name: name})

	finished := false
	defer func() {
		if finished {
			return
		}
		// Reached via panic or runtime.Goexit (t.Fatal / t.SkipNow).
		status, errText := outcome(tb, failedBefore)
		if r := recover(); r != nil {
			status = "failed"
			errText = fmt.Sprint(r)
			closeStep(tb, status, errText, time.Since(started))
			panic(r) // re-raised unchanged
		}
		closeStep(tb, status, errText, time.Since(started))
	}()

	fn()
	finished = true

	status, errText := outcome(tb, failedBefore)
	closeStep(tb, status, errText, time.Since(started))
}

func outcome(tb stepper, failedBefore bool) (status, errText string) {
	if tb.Failed() && !failedBefore {
		return "failed", ""
	}
	return "passed", ""
}

func closeStep(l logger, status, errText string, d time.Duration) {
	emitTo(l, sentinel.Message{
		Kind:     sentinel.KindStepStop,
		Status:   status,
		Error:    textutil.Truncate(errText, constants.MaxAttemptMessageRunes),
		Duration: d.Nanoseconds(),
	})
}
