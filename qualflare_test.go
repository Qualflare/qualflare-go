package qualflare

import (
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/sentinel"
)

// fakeTB stands in for testing.TB's log surface. testing.TB has an unexported
// method and cannot be implemented outside the testing package, which is
// exactly why emitTo depends on the narrow two-method interface instead.
type fakeTB struct {
	lines   []string
	helpers int
	panicOn bool
}

func (f *fakeTB) Log(args ...any) {
	if f.panicOn {
		panic("Log in goroutine after Test has completed")
	}
	if len(args) == 1 {
		if s, ok := args[0].(string); ok {
			f.lines = append(f.lines, s)
		}
	}
}
func (f *fakeTB) Helper() { f.helpers++ }

func (f *fakeTB) messages(t *testing.T) []sentinel.Message {
	t.Helper()
	var out []sentinel.Message
	for _, l := range f.lines {
		m, found, err := sentinel.Decode(l)
		if err != nil {
			t.Fatalf("emitted an undecodable line %q: %v", l, err)
		}
		if found {
			out = append(out, m)
		}
	}
	return out
}

func withGate(t *testing.T, on bool) {
	t.Helper()
	t.Setenv(EnvEnable, map[bool]string{true: "1", false: "0"}[on])
	gateOnce = onceReset()
}

func TestEmit_NeverPanicsWhenLogPanics(t *testing.T) {
	// t.Log genuinely panics with "Log in goroutine after Test has completed"
	// once the test and all its parents are done, synchronously on the caller's
	// goroutine. A dropped metadata line is correct; a crashed suite never is.
	withGate(t, true)
	f := &fakeTB{panicOn: true}
	emitTo(f, sentinel.Message{Kind: sentinel.KindLabel, Name: "team", Value: "platform"})
	// Reaching here at all is the assertion.
}

func TestEmit_MarksItselfAsAHelper(t *testing.T) {
	// Otherwise every metadata line is attributed to a line inside this
	// package rather than the user's test file.
	withGate(t, true)
	f := &fakeTB{}
	emitTo(f, sentinel.Message{Kind: sentinel.KindLabel, Name: "a", Value: "b"})
	if f.helpers == 0 {
		t.Error("emit should call Helper()")
	}
}

func TestEmit_OversizedMessageIsDroppedNotTruncated(t *testing.T) {
	withGate(t, true)
	f := &fakeTB{}
	emitTo(f, sentinel.Message{Kind: sentinel.KindDescription, Text: strings.Repeat("x", 100_000)})
	if len(f.lines) != 0 {
		t.Errorf("an unencodable message must emit nothing, got %d lines", len(f.lines))
	}
}

func TestAPI_EmitsNothingWhenDisabled(t *testing.T) {
	// Under plain `go test -v` there is no consumer, so metadata would be pure
	// noise in the developer's terminal.
	withGate(t, false)
	f := &fakeTB{}
	emitTo(f, sentinel.Message{Kind: sentinel.KindLabel})
	if Enabled() {
		t.Fatal("gate should be off")
	}
}

func TestStep_EmitsStartAndStopAroundTheBody(t *testing.T) {
	withGate(t, true)
	f := &fakeTB{}
	ran := false
	step(stepTB{f, false}, "add to cart", func() { ran = true })

	if !ran {
		t.Fatal("the body must run")
	}
	msgs := f.messages(t)
	if len(msgs) != 2 {
		t.Fatalf("expected start and stop, got %d", len(msgs))
	}
	if msgs[0].Kind != sentinel.KindStepStart || msgs[0].Name != "add to cart" {
		t.Errorf("start = %+v", msgs[0])
	}
	if msgs[1].Kind != sentinel.KindStepStop || msgs[1].Status != "passed" {
		t.Errorf("stop = %+v", msgs[1])
	}
	if msgs[1].Duration < 0 {
		t.Errorf("duration = %d", msgs[1].Duration)
	}
}

func TestStep_AlreadyFailedTestDoesNotMarkEveryLaterStepFailed(t *testing.T) {
	// The transition rule. Using tb.Failed() absolutely would mark every step
	// after the first failure as failed, however green the step itself was.
	withGate(t, true)
	f := &fakeTB{}
	step(stepTB{f, true}, "runs after an earlier failure", func() {})

	msgs := f.messages(t)
	if msgs[len(msgs)-1].Status != "passed" {
		t.Errorf("status = %q, want passed -- the step itself did not fail", msgs[len(msgs)-1].Status)
	}
}

func TestStep_RunsTheBodyEvenWhenDisabled(t *testing.T) {
	withGate(t, false)
	ran := false
	step(stepTB{&fakeTB{}, false}, "x", func() { ran = true })
	if !ran {
		t.Fatal("the body must run whether or not reporting is enabled")
	}
}

func TestStep_RepanicsTheUsersPanicUnchanged(t *testing.T) {
	// Swallowing a panic would turn a failing test green.
	withGate(t, true)
	f := &fakeTB{}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("the panic must be re-raised")
		}
		if r != "boom" {
			t.Errorf("panic value changed: %v", r)
		}
		msgs := f.messages(t)
		last := msgs[len(msgs)-1]
		if last.Kind != sentinel.KindStepStop || last.Status != "failed" {
			t.Errorf("the step should be recorded as failed before re-raising: %+v", last)
		}
		if last.Error != "boom" {
			t.Errorf("the panic value should be the step's error, got %q", last.Error)
		}
	}()

	step(stepTB{f, false}, "explodes", func() { panic("boom") })
}

func TestMaskedParameter_CannotCarryAValue(t *testing.T) {
	// A signature that cannot accept a secret cannot leak one.
	withGate(t, true)
	f := &fakeTB{}
	emitTo(f, sentinel.Message{Kind: sentinel.KindParameter, Name: "token", Masked: true})
	m := f.messages(t)[0]
	if m.Value != "" || !m.Masked {
		t.Errorf("got %+v", m)
	}
	if strings.Contains(strings.Join(f.lines, "\n"), "secret") {
		t.Error("nothing resembling a secret should appear on the wire")
	}
}
