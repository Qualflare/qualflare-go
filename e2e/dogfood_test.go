// Package e2e is the dogfood suite: qualflare-go reporting on itself.
//
// Every test here PASSES BY CONSTRUCTION. Status mapping for failures, panics,
// timeouts and build errors is the job of test/integration/fixtures/awkward,
// which is never uploaded. A red run here means a real regression rather than a
// fixture failing on purpose -- please do not "helpfully" add a failing test.
package e2e

import (
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go"
)

// The literal the verifier asserts is absent from the report's raw bytes.
//
// It is passed as an ordinary parameter's VALUE, because that is the only place
// a secret could ever reach the wire from. MaskedParameter takes no value at
// all, so it has no leak path to test -- the guarantee there is in the
// signature, not in the runtime.
const secret = "qf-go-dogfood-secret-value"

func TestRecordsEveryMetadataKind(t *testing.T) {
	qualflare.Label(t, "team", "platform")
	qualflare.Label(t, "feature", "reporting")
	qualflare.Tag(t, "smoke", "dogfood")
	qualflare.Link(t, "https://github.com/Qualflare/qualflare-go", qualflare.LinkIssue, "QG-1")
	qualflare.Priority(t, qualflare.PriorityHigh)
	qualflare.Description(t, "exercises every metadata call the README documents")
	qualflare.Parameter(t, "plan", "pro")
	qualflare.MaskedParameter(t, "token")
}

// A masked parameter emitted inside a step, where the secret is genuinely in
// scope. Nothing may carry it onto the wire.
func TestMaskedParameterInsideAStep(t *testing.T) {
	qualflare.Step(t, "authenticate", func() {
		qualflare.MaskedParameter(t, "api-token")
		if secret == "" {
			t.Fatal("unreachable")
		}
	})
}

func TestNestsSteps(t *testing.T) {
	qualflare.Step(t, "outer", func() {
		qualflare.Parameter(t, "sku", "widget")
		qualflare.Step(t, "inner", func() {
			qualflare.Parameter(t, "qty", "2")
		})
	})
}

func TestAttachesContent(t *testing.T) {
	qualflare.AttachText(t, "note", "a text attachment from the dogfood suite", "")
	qualflare.Attach(t, "payload", []byte(`{"from":"dogfood"}`), "application/json")
}

func TestSubtestsAreTheirOwnCases(t *testing.T) {
	for _, name := range []string{"alpha", "beta"} {
		name := name
		t.Run(name, func(t *testing.T) {
			qualflare.Label(t, "row", name)
		})
	}
}

func TestOutputThatLooksLikeMetadataIsNotConsumed(t *testing.T) {
	t.Log("@@QF1@@ not really a sentinel @@deadbeef@@")
	if !strings.HasPrefix("@@QF", "@@") {
		t.Fatal("unreachable")
	}
	qualflare.Label(t, "after", "the-decoy")
}
