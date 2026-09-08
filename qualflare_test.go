package qualflare

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/constants"
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

// --- the public surface ------------------------------------------------------

// Each exported call is checked for the message it produces. These are thin
// wrappers, which is exactly why they are worth pinning: a wrong Kind or a
// dropped field here is invisible until a report is already in front of a user.

func TestPublicAPI_MessageShapes(t *testing.T) {
	withGate(t, true)

	for _, tc := range []struct {
		name string
		emit func(*fakeTB)
		want sentinel.Message
	}{
		{"Label", func(f *fakeTB) { emitTo(f, labelMsg("team", "platform")) },
			sentinel.Message{Kind: sentinel.KindLabel, Name: "team", Value: "platform"}},
		{"Link with a type", func(f *fakeTB) { emitTo(f, linkMsg("https://x/1", LinkIssue, "QF-1")) },
			sentinel.Message{Kind: sentinel.KindLink, URL: "https://x/1", Type: "issue", Name: "QF-1"}},
		{"Link defaults to custom", func(f *fakeTB) { emitTo(f, linkMsg("https://x/1", "", "")) },
			sentinel.Message{Kind: sentinel.KindLink, URL: "https://x/1", Type: "custom"}},
		{"Description", func(f *fakeTB) { emitTo(f, descMsg("why")) },
			sentinel.Message{Kind: sentinel.KindDescription, Text: "why"}},
		{"Priority", func(f *fakeTB) { emitTo(f, prioMsg(PriorityHigh)) },
			sentinel.Message{Kind: sentinel.KindPriority, Value: "high"}},
		{"Parameter", func(f *fakeTB) { emitTo(f, paramMsg("plan", "pro")) },
			sentinel.Message{Kind: sentinel.KindParameter, Name: "plan", Value: "pro"}},
		{"MaskedParameter", func(f *fakeTB) { emitTo(f, maskedMsg("token")) },
			sentinel.Message{Kind: sentinel.KindParameter, Name: "token", Masked: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeTB{}
			tc.emit(f)
			got := f.messages(t)
			if len(got) != 1 {
				t.Fatalf("expected one message, got %d", len(got))
			}
			if got[0].Kind != tc.want.Kind || got[0].Name != tc.want.Name ||
				got[0].Value != tc.want.Value || got[0].URL != tc.want.URL ||
				got[0].Type != tc.want.Type || got[0].Text != tc.want.Text ||
				got[0].Masked != tc.want.Masked {
				t.Errorf("got %+v, want %+v", got[0], tc.want)
			}
		})
	}
}

func TestTag_ClipsAnOverlongTag(t *testing.T) {
	withGate(t, true)
	f := &fakeTB{}
	emitTo(f, tagMsg(strings.Repeat("x", 400)))
	got := f.messages(t)[0]
	if len(got.Tags) != 1 || len(got.Tags[0]) != constants.MaxTagLength {
		t.Errorf("tag length = %d, want the %d cap", len(got.Tags[0]), constants.MaxTagLength)
	}
}

func TestAttach_EncodesContentAsBase64(t *testing.T) {
	withGate(t, true)
	f := &fakeTB{}
	emitTo(f, attachMsg("payload", []byte("hello"), "application/json"))
	got := f.messages(t)[0]
	if got.Kind != sentinel.KindAttachment || got.MimeType != "application/json" {
		t.Errorf("got %+v", got)
	}
	if got.Content != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Errorf("content = %q", got.Content)
	}
}

func TestAttachText_DefaultsToPlainText(t *testing.T) {
	withGate(t, true)
	f := &fakeTB{}
	emitTo(f, attachMsg("note", []byte("hi"), "text/plain"))
	if got := f.messages(t)[0]; got.MimeType != "text/plain" {
		t.Errorf("mime = %q", got.MimeType)
	}
}

func TestTag_WithNoTagsEmitsNothing(t *testing.T) {
	withGate(t, true)
	f := &fakeTB{}
	Tag(nil) // a nil TB must be inert rather than panic
	if len(f.lines) != 0 {
		t.Error("expected no output")
	}
}

func TestEveryPublicCallIsInertWithANilTB(t *testing.T) {
	// A helper that forwards a TB it never received must not take the suite
	// down with it.
	Label(nil, "a", "b")
	Link(nil, "u", "", "")
	Tag(nil, "x")
	Description(nil, "d")
	Priority(nil, PriorityLow)
	Parameter(nil, "n", "v")
	MaskedParameter(nil, "n")
	Attach(nil, "n", []byte("x"), "")
	AttachText(nil, "n", "x", "")
	Step(nil, "s", func() {})
}
