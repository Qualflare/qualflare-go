package wire

import (
	"encoding/json"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/constants"
)

// These pin the rules that fail SILENTLY -- producing a report the server
// accepts and stores wrongly. Ported from qualflare-pytest's test_wire.py,
// plus the Go-specific omitempty traps that have no Python analogue.

func marshal(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func minimalCollect() Collect {
	return Collect{
		Framework:   "golang",
		Platform:    "api",
		OS:          "linux",
		Environment: "development",
		Language:    "en-US",
		Metadata:    Metadata{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z", CLIName: "qualflare-go"},
		Suites:      []Suite{},
	}
}

func TestCollect_CarriesTheTripleTheCLIDetectsOn(t *testing.T) {
	// `qf collect` identifies this format by framework + metadata + suites
	// together. Lose any one and it falls back to filename detection.
	m := marshal(t, minimalCollect())
	for _, k := range []string{"framework", "metadata", "suites"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q from the detection triple", k)
		}
	}
	if m["framework"] != "golang" {
		t.Errorf("framework = %v, want golang", m["framework"])
	}
}

func TestCollect_BranchCommitMilestoneArePresentAsNull(t *testing.T) {
	// The server distinguishes "not reported" from "absent". Dropping these
	// alongside the other empty optionals is silent and looks harmless.
	m := marshal(t, minimalCollect())
	for _, k := range []string{"branch", "commit", "milestone"} {
		v, ok := m[k]
		if !ok {
			t.Errorf("%q must be present even when unset", k)
			continue
		}
		if v != nil {
			t.Errorf("%q = %v, want null", k, v)
		}
	}
}

func TestCollect_EmptyCIFieldsAreOmittedRatherThanNulled(t *testing.T) {
	m := marshal(t, minimalCollect())
	for _, k := range []string{"ciProvider", "ciBuildNumber", "ciRunUrl", "ciPrNumber"} {
		if _, ok := m[k]; ok {
			t.Errorf("%q should be absent when unset", k)
		}
	}
}

func TestCollect_PopulatedValuesSurvive(t *testing.T) {
	c := minimalCollect()
	c.Branch, c.Commit, c.Milestone = StringPtr("main"), StringPtr("abc123"), IntPtr(7)
	c.CIProvider, c.CIPRNumber = "github", IntPtr(42)
	m := marshal(t, c)
	if m["branch"] != "main" || m["commit"] != "abc123" || m["milestone"] != float64(7) {
		t.Errorf("value-or-null fields did not round-trip: %v %v %v", m["branch"], m["commit"], m["milestone"])
	}
	if m["ciProvider"] != "github" || m["ciPrNumber"] != float64(42) {
		t.Errorf("CI fields did not round-trip")
	}
}

func TestCollect_MilestoneZeroIsKept(t *testing.T) {
	// Milestone 0 is a real milestone id, and a plain int would drop it.
	c := minimalCollect()
	c.Milestone = IntPtr(0)
	if m := marshal(t, c); m["milestone"] != float64(0) {
		t.Errorf("milestone = %v, want 0", m["milestone"])
	}
}

func TestSuite_CasesKeyIsAlwaysPresent(t *testing.T) {
	// A nil slice marshals to null, which the server rejects as a missing
	// required field -- 400ing an entire multi-file upload over one empty run.
	m := marshal(t, NewSuite("pkg", "golang"))
	v, ok := m["cases"]
	if !ok {
		t.Fatal("cases must always be present")
	}
	if v == nil {
		t.Fatal("cases marshalled to null; it must be an empty array")
	}
	if len(v.([]any)) != 0 {
		t.Fatalf("expected an empty array, got %v", v)
	}
}

func TestSuite_NilCasesWouldMarshalToNull(t *testing.T) {
	// Documents exactly why NewSuite exists. If this ever stops being true the
	// constructor is no longer load-bearing and the comment should change.
	if m := marshal(t, Suite{Name: "pkg", Category: "golang"}); m["cases"] != nil {
		t.Fatal("expected a nil Cases slice to marshal to null")
	}
}

func TestTrimAttempts_DropsALoneAttempt(t *testing.T) {
	// Below two the server persists nothing, so a one-element array is body
	// bytes spent on a row that is discarded. The rule lives here rather than
	// in an omitempty tag, which would happily send an array of one.
	if got := TrimAttempts([]Attempt{{Attempt: 1, Status: "passed"}}); got != nil {
		t.Errorf("a lone attempt must be dropped, got %v", got)
	}
	if got := TrimAttempts(nil); got != nil {
		t.Errorf("no attempts must stay nil, got %v", got)
	}
}

func TestTrimAttempts_KeepsTwo(t *testing.T) {
	in := []Attempt{{Attempt: 1, Status: "failed"}, {Attempt: 2, Status: "passed"}}
	if got := TrimAttempts(in); len(got) != 2 {
		t.Errorf("expected both attempts, got %d", len(got))
	}
}

func TestTrimAttempts_KeepsTheFinalAttemptWhenClamping(t *testing.T) {
	// A plain head-slice would discard the attempt that carries the outcome.
	in := make([]Attempt, 0, 80)
	for i := 1; i <= 80; i++ {
		in = append(in, Attempt{Attempt: i, Status: "failed"})
	}
	in[len(in)-1].Status = "passed"
	got := TrimAttempts(in)
	if len(got) != constants.MaxAttemptsPerCase {
		t.Fatalf("expected %d attempts, got %d", constants.MaxAttemptsPerCase, len(got))
	}
	if got[len(got)-1].Attempt != 80 || got[len(got)-1].Status != "passed" {
		t.Errorf("final attempt not preserved: %+v", got[len(got)-1])
	}
	if got[len(got)-2].Attempt != constants.MaxAttemptsPerCase-1 {
		t.Errorf("head not preserved: %+v", got[len(got)-2])
	}
}

func TestCase_AttemptsSetViaTrimAreOmittedWhenLone(t *testing.T) {
	c := Case{ID: "p.T", Name: "T", Status: "passed"}
	c.Attempts = TrimAttempts([]Attempt{{Attempt: 1, Status: "passed"}})
	if _, ok := marshal(t, c)["attempts"]; ok {
		t.Error("a lone attempt must not reach the wire")
	}
}

func TestCase_TwoAttemptsAreSentInFull(t *testing.T) {
	c := Case{ID: "p.T", Name: "T", Status: "passed", Attempts: []Attempt{
		{Attempt: 1, Status: "failed", Message: "boom"},
		{Attempt: 2, Status: "passed"},
	}}
	a, ok := marshal(t, c)["attempts"].([]any)
	if !ok || len(a) != 2 {
		t.Fatalf("expected 2 attempts, got %v", marshal(t, c)["attempts"])
	}
}

func TestCase_FalseIsFlakySurvives(t *testing.T) {
	// A test that failed after retries must report isFlaky=false rather than
	// saying nothing about flakiness. A plain bool with omitempty drops it.
	c := Case{ID: "p.T", Name: "T", Status: "failed", IsFlaky: BoolPtr(false), RetryCount: IntPtr(0)}
	m := marshal(t, c)
	if m["isFlaky"] != false {
		t.Errorf("isFlaky = %v, want false", m["isFlaky"])
	}
	if m["retryCount"] != float64(0) {
		t.Errorf("retryCount = %v, want 0", m["retryCount"])
	}
}

func TestCase_ShardIndexZeroIsKept(t *testing.T) {
	// Shard 0 is a real shard. Dropping it silently merges its cases into
	// "unsharded".
	c := Case{ID: "p.T", Name: "T", Status: "passed", ShardIndex: IntPtr(0)}
	if m := marshal(t, c); m["shardIndex"] != float64(0) {
		t.Errorf("shardIndex = %v, want 0", m["shardIndex"])
	}
}

func TestCase_UnsetOptionalsAreAbsent(t *testing.T) {
	m := marshal(t, Case{ID: "p.T", Name: "T", Status: "passed"})
	for _, k := range []string{"error", "description", "priority", "retryCount", "isFlaky",
		"shardIndex", "startedAt", "properties", "tags", "labels", "links", "steps", "attachments", "attempts"} {
		if _, ok := m[k]; ok {
			t.Errorf("%q should be absent on a minimal case", k)
		}
	}
}

func TestCase_ZeroDurationIsStillReported(t *testing.T) {
	// duration has no omitempty: a genuinely instant test reports 0, not
	// nothing.
	if m := marshal(t, Case{ID: "p.T", Name: "T", Status: "passed"}); m["duration"] != float64(0) {
		t.Errorf("duration = %v, want 0", m["duration"])
	}
}

func TestStep_ParentIndexZeroIsKept(t *testing.T) {
	// THE falsy trap: index 0 is the first step, and a child that loses
	// parentIndex renders as a sibling.
	s := Step{Name: "child", Status: "passed", ParentIndex: IntPtr(0)}
	if m := marshal(t, s); m["parentIndex"] != float64(0) {
		t.Errorf("parentIndex = %v, want 0", m["parentIndex"])
	}
}

func TestStep_TopLevelHasNoParentIndex(t *testing.T) {
	if _, ok := marshal(t, Step{Name: "top", Status: "passed"})["parentIndex"]; ok {
		t.Error("a top-level step must not carry parentIndex")
	}
}

func TestParameter_MaskedCarriesNoValue(t *testing.T) {
	// The whole point of masking: the secret must not reach the report file.
	m := marshal(t, Parameter{Name: "token", Masked: true})
	if _, ok := m["value"]; ok {
		t.Error("a masked parameter must not carry a value")
	}
	if m["masked"] != true {
		t.Error("masked must be true")
	}
}

func TestParameter_EmptyStringValueIsPreserved(t *testing.T) {
	// Distinct from "no value": the author passed something.
	m := marshal(t, Parameter{Name: "note", Value: StringPtr("")})
	v, ok := m["value"]
	if !ok || v != "" {
		t.Errorf("empty-string value = %v (present=%v), want \"\"", v, ok)
	}
}

func TestAttachment_ImageTravelsByPath(t *testing.T) {
	a := Attachment{Name: "shot", MimeType: "image/png", LocalImagePath: "ab-shot.png", FileSize: Int64Ptr(12)}
	m := marshal(t, a)
	if m["localImagePath"] != "ab-shot.png" || m["fileSize"] != float64(12) {
		t.Errorf("image attachment did not round-trip: %v", m)
	}
	if _, ok := m["content"]; ok {
		t.Error("an image referenced by path must not also be inlined")
	}
}

func TestAttachment_StepIndexZeroIsKept(t *testing.T) {
	if m := marshal(t, Attachment{Name: "a", StepIndex: IntPtr(0)}); m["stepIndex"] != float64(0) {
		t.Errorf("stepIndex = %v, want 0", m["stepIndex"])
	}
}

func TestWholePayloadIsJSONSerialisable(t *testing.T) {
	c := minimalCollect()
	s := NewSuite("pkg", "golang")
	s.Cases = append(s.Cases, Case{
		ID: "pkg.T", Name: "T", Status: "passed", Duration: 10,
		Labels: []Label{{Name: "team", Value: "platform"}},
		Steps:  []Step{{Name: "s", Status: "passed"}},
		Attempts: []Attempt{
			{Attempt: 1, Status: "failed"},
			{Attempt: 2, Status: "passed"},
		},
	})
	c.Suites = append(c.Suites, s)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Collect
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if back.Suites[0].Cases[0].Name != "T" {
		t.Error("payload did not survive a round trip")
	}
}
