package replay

import (
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
)

func start(name string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindStepStart, Name: name}
}
func stop(status string, dur int64) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindStepStop, Status: status, Duration: dur}
}
func param(name, value string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindParameter, Name: name, Value: value}
}

func TestSteps_RecordStatusAndDuration(t *testing.T) {
	got := Replay([]sentinel.Message{start("outer"), stop("passed", 5_000_000)})
	if len(got.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(got.Steps))
	}
	s := got.Steps[0]
	if s.Name != "outer" || s.Status != "passed" || s.Duration != 5_000_000 {
		t.Errorf("step = %+v", s)
	}
	if s.ParentIndex != nil {
		t.Error("a top-level step must have no parent")
	}
}

func TestSteps_NestingPointsAtTheParent(t *testing.T) {
	got := Replay([]sentinel.Message{start("outer"), start("inner"), stop("passed", 1), stop("passed", 2)})
	if len(got.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(got.Steps))
	}
	if got.Steps[0].ParentIndex != nil {
		t.Error("outer should have no parent")
	}
	if got.Steps[1].ParentIndex == nil || *got.Steps[1].ParentIndex != 0 {
		t.Errorf("inner parentIndex = %v, want 0", got.Steps[1].ParentIndex)
	}
}

func TestSteps_SiblingsShareAParent(t *testing.T) {
	got := Replay([]sentinel.Message{
		start("outer"), start("a"), stop("passed", 1), start("b"), stop("passed", 1), stop("passed", 1),
	})
	if len(got.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(got.Steps))
	}
	for _, i := range []int{1, 2} {
		if got.Steps[i].ParentIndex == nil || *got.Steps[i].ParentIndex != 0 {
			t.Errorf("step %d parentIndex = %v, want 0", i, got.Steps[i].ParentIndex)
		}
	}
}

func TestSteps_AnUnmatchedStopIsIgnored(t *testing.T) {
	if got := Replay([]sentinel.Message{stop("passed", 1)}); len(got.Steps) != 0 {
		t.Errorf("expected no steps, got %d", len(got.Steps))
	}
}

func TestSteps_AFailingStepKeepsItsError(t *testing.T) {
	got := Replay([]sentinel.Message{
		start("boom"),
		{Kind: sentinel.KindStepStop, Status: "failed", Error: "assertion failed", Duration: 3},
	})
	if got.Steps[0].Status != "failed" || got.Steps[0].Error != "assertion failed" {
		t.Errorf("step = %+v", got.Steps[0])
	}
}

func TestStepCap_OuterStepKeepsItsOwnDuration(t *testing.T) {
	// THE regression the sentinel exists for. Without a placeholder push, the
	// dropped steps' matching stops close the legitimately-open outer step,
	// overwriting its status and duration -- and then its real stop is
	// discarded because the stack is empty. Measured in the pytest reporter as
	// an outer step around a 50ms sleep reporting 0.000ms.
	msgs := []sentinel.Message{start("outer")}
	for i := 0; i < constants.MaxStepsPerTestAttempt+100; i++ {
		msgs = append(msgs, start("inner"), stop("passed", 1))
	}
	msgs = append(msgs, stop("passed", 50_000_000))

	got := Replay(msgs)

	if len(got.Steps) != constants.MaxStepsPerTestAttempt {
		t.Fatalf("steps = %d, want the cap %d", len(got.Steps), constants.MaxStepsPerTestAttempt)
	}
	outer := got.Steps[0]
	if outer.Name != "outer" {
		t.Fatalf("first step = %q, want outer", outer.Name)
	}
	if outer.Duration != 50_000_000 {
		t.Errorf("outer duration = %d, want 50000000 -- the cap sentinel failed", outer.Duration)
	}
	if len(got.Warnings) == 0 {
		t.Error("dropping steps should warn")
	}
}

func TestStepCap_KeepsTheFirstStepsNotAScrambledSelection(t *testing.T) {
	msgs := []sentinel.Message{start("outer")}
	for i := 0; i < constants.MaxStepsPerTestAttempt+10; i++ {
		msgs = append(msgs, start("inner"), stop("passed", 1))
	}
	msgs = append(msgs, stop("passed", 1))
	got := Replay(msgs)
	if got.Steps[0].Name != "outer" || got.Steps[1].Name != "inner" {
		t.Errorf("kept steps start with %q, %q", got.Steps[0].Name, got.Steps[1].Name)
	}
}

func TestParameters_OutsideAStepBelongToTheCase(t *testing.T) {
	got := Replay([]sentinel.Message{param("plan", "pro")})
	if len(got.CaseParameters) != 1 || got.CaseParameters[0].Name != "plan" {
		t.Fatalf("case parameters = %+v", got.CaseParameters)
	}
	if got.CaseParameters[0].Value == nil || *got.CaseParameters[0].Value != "pro" {
		t.Errorf("value = %v", got.CaseParameters[0].Value)
	}
}

func TestParameters_InsideAStepBelongToThatStep(t *testing.T) {
	got := Replay([]sentinel.Message{start("outer"), param("sku", "widget"), stop("passed", 1)})
	if len(got.CaseParameters) != 0 {
		t.Errorf("expected no case parameters, got %+v", got.CaseParameters)
	}
	if len(got.Steps[0].Parameters) != 1 || got.Steps[0].Parameters[0].Name != "sku" {
		t.Errorf("step parameters = %+v", got.Steps[0].Parameters)
	}
}

func TestParameters_LandOnTheInnermostOpenStep(t *testing.T) {
	got := Replay([]sentinel.Message{
		start("outer"), start("inner"), param("sku", "widget"), stop("passed", 1), stop("passed", 1),
	})
	if len(got.Steps[0].Parameters) != 0 {
		t.Error("the outer step should not receive the parameter")
	}
	if len(got.Steps[1].Parameters) != 1 {
		t.Errorf("inner parameters = %+v", got.Steps[1].Parameters)
	}
}

func TestParameters_PastTheCapSentinelStillReachTheRealOpenStep(t *testing.T) {
	// A parameter emitted while a dropped step is "open" must attach to the
	// innermost REAL step, not vanish and not land on the case.
	msgs := []sentinel.Message{start("outer")}
	for i := 0; i < constants.MaxStepsPerTestAttempt+5; i++ {
		msgs = append(msgs, start("inner"), stop("passed", 1))
	}
	msgs = append(msgs, start("dropped"), param("late", "value"), stop("passed", 1), stop("passed", 1))

	got := Replay(msgs)
	if len(got.CaseParameters) != 0 {
		t.Errorf("the parameter should not fall through to the case: %+v", got.CaseParameters)
	}
	found := false
	for _, s := range got.Steps {
		for _, p := range s.Parameters {
			if p.Name == "late" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the parameter was lost entirely")
	}
}

func TestMaskedParameter_CarriesNoValue(t *testing.T) {
	got := Replay([]sentinel.Message{{Kind: sentinel.KindParameter, Name: "token", Masked: true}})
	p := got.CaseParameters[0]
	if !p.Masked {
		t.Error("masked flag lost")
	}
	if p.Value != nil {
		t.Errorf("a masked parameter must carry no value, got %q", *p.Value)
	}
}

func TestSimpleMetadata_Accumulates(t *testing.T) {
	got := Replay([]sentinel.Message{
		{Kind: sentinel.KindLabel, Name: "team", Value: "platform"},
		{Kind: sentinel.KindTag, Tags: []string{"smoke", "checkout"}},
		{Kind: sentinel.KindLink, URL: "https://example.com/1", Type: "issue", Name: "QF-1"},
		{Kind: sentinel.KindDescription, Text: "why this matters"},
		{Kind: sentinel.KindPriority, Value: "high"},
	})
	if len(got.Labels) != 1 || got.Labels[0].Value != "platform" {
		t.Errorf("labels = %+v", got.Labels)
	}
	if strings.Join(got.Tags, ",") != "smoke,checkout" {
		t.Errorf("tags = %v", got.Tags)
	}
	if got.Links[0].Type != "issue" {
		t.Errorf("links = %+v", got.Links)
	}
	if got.Description != "why this matters" || got.Priority != "high" {
		t.Errorf("description=%q priority=%q", got.Description, got.Priority)
	}
}

func TestLink_DefaultsToCustom(t *testing.T) {
	got := Replay([]sentinel.Message{{Kind: sentinel.KindLink, URL: "https://example.com/1"}})
	if got.Links[0].Type != "custom" {
		t.Errorf("type = %q, want custom", got.Links[0].Type)
	}
}

func TestPriority_UnrecognisedIsDropped(t *testing.T) {
	// The server normalises it away anyway, so a bad value is not worth a row.
	if got := Replay([]sentinel.Message{{Kind: sentinel.KindPriority, Value: "URGENT!"}}); got.Priority != "" {
		t.Errorf("priority = %q, want empty", got.Priority)
	}
}

func TestCaps_AreApplied(t *testing.T) {
	var msgs []sentinel.Message
	for i := 0; i < constants.MaxLabelsPerCase+20; i++ {
		msgs = append(msgs, sentinel.Message{Kind: sentinel.KindLabel, Name: "n", Value: "v"})
	}
	for i := 0; i < constants.MaxLinksPerCase+20; i++ {
		msgs = append(msgs, sentinel.Message{Kind: sentinel.KindLink, URL: "u"})
	}
	for i := 0; i < constants.MaxTagsPerCase+20; i++ {
		msgs = append(msgs, sentinel.Message{Kind: sentinel.KindTag, Tags: []string{"t"}})
	}
	got := Replay(msgs)
	if len(got.Labels) != constants.MaxLabelsPerCase {
		t.Errorf("labels = %d, want %d", len(got.Labels), constants.MaxLabelsPerCase)
	}
	if len(got.Links) != constants.MaxLinksPerCase {
		t.Errorf("links = %d, want %d", len(got.Links), constants.MaxLinksPerCase)
	}
	if len(got.Tags) != constants.MaxTagsPerCase {
		t.Errorf("tags = %d, want %d", len(got.Tags), constants.MaxTagsPerCase)
	}
}

func TestParameterCap_PerStep(t *testing.T) {
	msgs := []sentinel.Message{start("outer")}
	for i := 0; i < constants.MaxParametersPerStep+20; i++ {
		msgs = append(msgs, param("p", "v"))
	}
	msgs = append(msgs, stop("passed", 1))
	got := Replay(msgs)
	if len(got.Steps[0].Parameters) != constants.MaxParametersPerStep {
		t.Errorf("parameters = %d, want %d", len(got.Steps[0].Parameters), constants.MaxParametersPerStep)
	}
}

func TestAttachment_OversizedIsDroppedNotTruncated(t *testing.T) {
	got := Replay([]sentinel.Message{{
		Kind: sentinel.KindAttachment, Name: "huge",
		Content: strings.Repeat("A", constants.MaxAttachmentInlineChars+10),
	}})
	if len(got.Attachments) != 1 {
		t.Fatalf("the row should survive, got %d attachments", len(got.Attachments))
	}
	if got.Attachments[0].Content != "" {
		t.Error("an oversized attachment must carry no content")
	}
	if len(got.Warnings) == 0 {
		t.Error("dropping an attachment should warn")
	}
}

func TestAttachment_InlineContentIsKept(t *testing.T) {
	got := Replay([]sentinel.Message{{
		Kind: sentinel.KindAttachment, Name: "note", MimeType: "text/plain", Content: "aGVsbG8",
	}})
	if got.Attachments[0].Content != "aGVsbG8" || got.Attachments[0].MimeType != "text/plain" {
		t.Errorf("attachment = %+v", got.Attachments[0])
	}
}
