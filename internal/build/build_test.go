package build

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/config"
	"github.com/Qualflare/qualflare-go/internal/model"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
	"github.com/Qualflare/qualflare-go/internal/stream"
)

// Helpers to build a run model directly. Driving these through a real `go test`
// covers the shapes a normal suite produces; the branches that matter most --
// a parent failing while its children pass, a -count collapse, a package that
// failed with nothing to blame -- are far easier to state as data.

func occ(status string, nanos int64, lines ...string) *model.Occurrence {
	return &model.Occurrence{Status: status, ElapsedNanos: nanos, Lines: lines}
}

func pkg(name string, tests ...*model.Test) *model.Package {
	p := &model.Package{Name: name, Tests: map[string]*model.Test{}}
	for _, t := range tests {
		p.Tests[t.Name] = t
		p.TestOrder = append(p.TestOrder, t.Name)
	}
	return p
}

func test(name string, occs ...*model.Occurrence) *model.Test {
	return &model.Test{Name: name, Occurrences: occs}
}

func sentinelLine(t *testing.T, m sentinel.Message) string {
	t.Helper()
	line, err := sentinel.Encode(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return "    x_test.go:1: " + line
}

func run(pkgs ...*model.Package) *model.Run {
	r := &model.Run{Packages: map[string]*model.Package{}, BuildOutput: map[string][]string{}}
	for _, p := range pkgs {
		r.Packages[p.Name] = p
		r.PackageOrder = append(r.PackageOrder, p.Name)
	}
	return r
}

func names(cases []interface{ GetName() string }) []string { return nil } // unused placeholder

// --- parent handling ---------------------------------------------------------

func TestParent_PassingParentWithSubtestsIsDropped(t *testing.T) {
	// Go's Elapsed for a parent already includes its children, and every child
	// is reported separately, so keeping it double-counts.
	p := pkg("m",
		test("TestParent", occ(model.StatusPassed, 10)),
		test("TestParent/a", occ(model.StatusPassed, 5)),
	)
	if _, ok := Cases(p, p.Tests["TestParent"], Options{}); ok {
		t.Error("a passing parent with subtests should be dropped")
	}
	if _, ok := Cases(p, p.Tests["TestParent/a"], Options{}); !ok {
		t.Error("the subtest must be kept")
	}
}

func TestParent_FailingParentWithPassingChildrenIsKept(t *testing.T) {
	// The case qualflare-cli's parser drops unconditionally, losing the only
	// thing that actually failed.
	p := pkg("m",
		test("TestParent", occ(model.StatusFailed, 10, "    x_test.go:9: parent assertion failed")),
		test("TestParent/a", occ(model.StatusPassed, 5)),
	)
	got, ok := Cases(p, p.Tests["TestParent"], Options{})
	if !ok {
		t.Fatal("a parent that failed while its children passed must be kept")
	}
	if got[0].Status != model.StatusFailed {
		t.Errorf("status = %s", got[0].Status)
	}
	if got[0].Duration != 0 {
		t.Errorf("a kept parent must carry no duration (its children own it), got %d", got[0].Duration)
	}
}

func TestParent_FailingParentIsDroppedWhenAChildAlsoFailed(t *testing.T) {
	// The child explains the failure, so the parent adds nothing but a
	// duplicate.
	p := pkg("m",
		test("TestParent", occ(model.StatusFailed, 10)),
		test("TestParent/a", occ(model.StatusFailed, 5)),
	)
	if _, ok := Cases(p, p.Tests["TestParent"], Options{}); ok {
		t.Error("the parent should be dropped when a child already failed")
	}
}

func TestParent_ParentCarryingMetadataIsKept(t *testing.T) {
	// Hoisting a parent's labels onto its children would duplicate them and
	// inflate counts, so the parent is kept instead.
	line := sentinelLine(t, sentinel.Message{Kind: sentinel.KindLabel, Name: "team", Value: "platform"})
	p := pkg("m",
		test("TestParent", occ(model.StatusPassed, 10, line)),
		test("TestParent/a", occ(model.StatusPassed, 5)),
	)
	got, ok := Cases(p, p.Tests["TestParent"], Options{})
	if !ok {
		t.Fatal("a parent carrying metadata must be kept")
	}
	if len(got[0].Labels) != 1 {
		t.Errorf("labels = %+v", got[0].Labels)
	}
}

// --- -count collapse and split ----------------------------------------------

func TestCount_CollapseFoldsOccurrencesIntoAttempts(t *testing.T) {
	p := pkg("m", test("TestFlaky",
		occ(model.StatusFailed, 5, "    x_test.go:9: boom"),
		occ(model.StatusPassed, 6),
		occ(model.StatusPassed, 7),
	))
	got, ok := Cases(p, p.Tests["TestFlaky"], Options{Repeat: "collapse"})
	if !ok || len(got) != 1 {
		t.Fatalf("collapse should produce one case, got %d", len(got))
	}
	c := got[0]
	if len(c.Attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(c.Attempts))
	}
	if c.RetryCount == nil || *c.RetryCount != 2 {
		t.Errorf("retryCount = %v, want 2", c.RetryCount)
	}
	if c.IsFlaky == nil || !*c.IsFlaky {
		t.Error("a run that ended green after failing is flaky")
	}
	// The final attempt IS the case: api-service migration 0243 makes this an
	// invariant, so summing durations would violate it.
	if c.Duration != 7 {
		t.Errorf("duration = %d, want the final attempt's 7", c.Duration)
	}
	if c.Attempts[2].Duration == nil || *c.Attempts[2].Duration != c.Duration {
		t.Error("the final attempt's duration must mirror the case's")
	}
	if c.Attempts[2].Status != c.Status {
		t.Error("the final attempt's status must mirror the case's")
	}
}

func TestCount_NeverFabricatesFlakinessWhenEveryRunPassed(t *testing.T) {
	p := pkg("m", test("TestSteady", occ(model.StatusPassed, 1), occ(model.StatusPassed, 2)))
	got, _ := Cases(p, p.Tests["TestSteady"], Options{Repeat: "collapse"})
	if got[0].IsFlaky != nil {
		t.Errorf("isFlaky = %v, want unset -- nothing failed", *got[0].IsFlaky)
	}
}

func TestCount_FailingAfterEveryRetryIsNotFlaky(t *testing.T) {
	p := pkg("m", test("TestBroken", occ(model.StatusFailed, 1), occ(model.StatusFailed, 2)))
	got, _ := Cases(p, p.Tests["TestBroken"], Options{Repeat: "collapse"})
	if got[0].IsFlaky != nil && *got[0].IsFlaky {
		t.Error("failing after retries is failing, not flaky")
	}
}

func TestCount_SplitEmitsOneCasePerOccurrence(t *testing.T) {
	// `-count=20` is a flake hunt, and "the last run passed" is a misleading
	// summary of it. Split must actually split.
	p := pkg("m", test("TestFlaky",
		occ(model.StatusFailed, 5),
		occ(model.StatusPassed, 6),
		occ(model.StatusPassed, 7),
	))
	got, ok := Cases(p, p.Tests["TestFlaky"], Options{Repeat: "split"})
	if !ok {
		t.Fatal("split should produce cases")
	}
	if len(got) != 3 {
		t.Fatalf("split produced %d cases, want one per occurrence (3)", len(got))
	}
	if got[0].Status != model.StatusFailed || got[1].Status != model.StatusPassed {
		t.Errorf("statuses = %s, %s, %s", got[0].Status, got[1].Status, got[2].Status)
	}
	ids := map[string]bool{}
	for _, c := range got {
		if ids[c.ID] {
			t.Errorf("duplicate id %q -- the server would merge these", c.ID)
		}
		ids[c.ID] = true
		if len(c.Attempts) != 0 {
			t.Error("a split case represents one run and carries no attempts")
		}
	}
}

// --- package-level failures --------------------------------------------------

func TestPackageFailure_SynthesisedWhenNothingElseExplainsIt(t *testing.T) {
	// Without this a broken build uploads a green launch.
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	p.Status = "fail"
	p.Lines = []string{"FAIL\tm\t0.1s"}

	s, ok := Suite(run(p), p, Options{})
	if !ok {
		t.Fatal("expected a suite")
	}
	if len(s.Cases) != 2 {
		t.Fatalf("expected the passing case plus a synthetic failure, got %d", len(s.Cases))
	}
	if !strings.Contains(s.Cases[1].Name, "package failure") {
		t.Errorf("case = %q", s.Cases[1].Name)
	}
}

func TestPackageFailure_NotSynthesisedWhenACaseAlreadyFailed(t *testing.T) {
	p := pkg("m", test("TestFails", occ(model.StatusFailed, 1)))
	p.Status = "fail"
	s, _ := Suite(run(p), p, Options{})
	if len(s.Cases) != 1 {
		t.Errorf("the failure is already attributed; got %d cases", len(s.Cases))
	}
}

func TestPackageFailure_NotSynthesisedWhenAnAttemptExplainsIt(t *testing.T) {
	// Under -count a flaky test makes the PACKAGE fail while every final case
	// status is passed. The failure is attributed -- to attempt 1 -- so
	// synthesising another one reports a phantom error on every flaky run.
	p := pkg("m", test("TestFlaky", occ(model.StatusFailed, 1), occ(model.StatusPassed, 2)))
	p.Status = "fail"

	s, _ := Suite(run(p), p, Options{Repeat: "collapse"})
	for _, c := range s.Cases {
		if strings.Contains(c.Name, "package failure") {
			t.Fatalf("a failing attempt already explains the package failure; got a phantom %q", c.Name)
		}
	}
	if len(s.Cases) != 1 {
		t.Errorf("expected just the flaky case, got %d", len(s.Cases))
	}
}

func TestPackageFailure_BuildFailureCarriesTheCompilerOutput(t *testing.T) {
	// The join is FailedBuild -> build event ImportPath. The ImportPath is the
	// test BINARY path and never equals Package, so a Package-equality join
	// matches nothing.
	p := pkg("m/broken")
	p.Status = "fail"
	p.FailedBuild = "m/broken.test"
	r := run(p)
	r.BuildOutput["m/broken.test"] = []string{"broken_test.go:6:7: expected ';', found is\n"}

	s, ok := Suite(r, p, Options{})
	if !ok {
		t.Fatal("expected a suite")
	}
	if !strings.Contains(s.Cases[0].Name, "build failure") {
		t.Errorf("name = %q", s.Cases[0].Name)
	}
	if !strings.Contains(s.Cases[0].Error, "expected ';'") {
		t.Errorf("the compiler error must survive, got %q", s.Cases[0].Error)
	}
}

func TestSuite_PackageWithNoCasesIsNotReported(t *testing.T) {
	// A package with no test files would otherwise add a meaningless green
	// entry per package across a monorepo.
	p := pkg("m/notests")
	p.Status = "skip"
	if _, ok := Suite(run(p), p, Options{}); ok {
		t.Error("a package that contributed nothing should not become a suite")
	}
}

// --- the backstop ------------------------------------------------------------

func TestBackstop_SynthesisesAFailureWhenTheChildFailedButNothingIsRed(t *testing.T) {
	// Measured on Go 1.21/1.23: a package that fails to compile produces no
	// fail event at all, so every event-derived rule misses it.
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	exit := 1
	got := Collect(Input{
		Run: run(p), Cfg: config.Config{Framework: "golang"},
		Preamble: "# m/broken\nbroken_test.go:6:7: expected ';', found is",
		ExitCode: &exit,
	})

	var red int
	var evidence string
	for _, s := range got.Suites {
		for _, c := range s.Cases {
			if c.Status != model.StatusPassed {
				red++
				evidence = c.Error
			}
		}
	}
	if red == 0 {
		t.Fatal("go test exited 1 and the report was all green -- the backstop did not fire")
	}
	if !strings.Contains(evidence, "expected ';'") {
		t.Errorf("the backstop should carry the stderr evidence, got %q", evidence)
	}
}

func TestBackstop_StaysQuietWhenAFailureIsAlreadyReported(t *testing.T) {
	p := pkg("m", test("TestFails", occ(model.StatusFailed, 1)))
	exit := 1
	got := Collect(Input{Run: run(p), Cfg: config.Config{Framework: "golang"}, ExitCode: &exit})
	for _, s := range got.Suites {
		if s.Name == "[unattributed]" {
			t.Error("the failure is already attributed; the backstop must not add another")
		}
	}
}

func TestBackstop_StaysQuietOnASuccessfulRun(t *testing.T) {
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	exit := 0
	got := Collect(Input{Run: run(p), Cfg: config.Config{Framework: "golang"}, ExitCode: &exit})
	if len(got.Suites) != 1 {
		t.Errorf("suites = %d, want 1", len(got.Suites))
	}
}

func TestBackstop_CannotFireWhenTheExitCodeIsUnknown(t *testing.T) {
	// Pipe and file modes cannot see it, which is why the wrapper is the
	// documented form.
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	got := Collect(Input{Run: run(p), Cfg: config.Config{Framework: "golang"}, ExitCode: nil})
	if len(got.Suites) != 1 {
		t.Errorf("suites = %d, want 1", len(got.Suites))
	}
}

// --- output handling ---------------------------------------------------------

func TestOutput_SentinelLinesAreStrippedFromTheErrorText(t *testing.T) {
	line := sentinelLine(t, sentinel.Message{Kind: sentinel.KindLabel, Name: "team", Value: "platform"})
	p := pkg("m", test("TestFails", occ(model.StatusFailed, 1,
		"=== RUN   TestFails", line, "    x_test.go:9: real failure text")))

	got, _ := Cases(p, p.Tests["TestFails"], Options{})
	if strings.Contains(got[0].Error, "@@QF") {
		t.Error("metadata must not pollute the case's error text")
	}
	if strings.Contains(got[0].Error, "=== RUN") {
		t.Error("runner framing is not the test's output")
	}
	if !strings.Contains(got[0].Error, "real failure text") {
		t.Errorf("the real failure text was lost: %q", got[0].Error)
	}
}

func TestOutput_ALineThatLooksLikeMetadataButIsBrokenSurvives(t *testing.T) {
	// Never silently consume a user's log line.
	p := pkg("m", test("TestFails", occ(model.StatusFailed, 1, "@@QF1@@bogus@@00000000@@")))
	got, _ := Cases(p, p.Tests["TestFails"], Options{})
	if !strings.Contains(got[0].Error, "@@QF1@@bogus") {
		t.Errorf("the undecodable line should remain visible, got %q", got[0].Error)
	}
}

func TestMasking_ParameterValueNeverReachesThePayload(t *testing.T) {
	const secret = "s3cret-value"
	masked := sentinelLine(t, sentinel.Message{Kind: sentinel.KindParameter, Name: "token", Masked: true})
	plain := sentinelLine(t, sentinel.Message{Kind: sentinel.KindParameter, Name: "plan", Value: "pro"})
	p := pkg("m", test("TestThing", occ(model.StatusPassed, 1, masked, plain)))

	got, _ := Cases(p, p.Tests["TestThing"], Options{})
	if got[0].Properties["token"] != "••••••" {
		t.Errorf("masked property = %q", got[0].Properties["token"])
	}
	if got[0].Properties["plan"] != "pro" {
		t.Errorf("plain property = %q", got[0].Properties["plan"])
	}
	raw, _ := json.Marshal(got[0])
	if strings.Contains(string(raw), secret) {
		t.Error("a secret reached the payload")
	}
}

func TestCollect_CarriesTheDetectionTripleAndValueOrNull(t *testing.T) {
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	got := Collect(Input{Run: run(p), Cfg: config.Config{Framework: "golang", Platform: "api"}, Stats: stream.Stats{}})

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"framework", "metadata", "suites"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q from the detection triple", k)
		}
	}
	for _, k := range []string{"branch", "commit", "milestone"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%q must be present as value-or-null", k)
		}
	}
}

func TestCase_ShardIndexIsAppliedWhenConfigured(t *testing.T) {
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	zero := 0
	got, _ := Cases(p, p.Tests["TestPasses"], Options{ShardIndex: &zero})
	if got[0].ShardIndex == nil || *got[0].ShardIndex != 0 {
		t.Errorf("shard 0 is a real shard; got %v", got[0].ShardIndex)
	}
}

// --- diagnostics -------------------------------------------------------------

func TestWarnings_StepsDroppedAtTheCapAreReported(t *testing.T) {
	// Previously collected and discarded: a test that silently lost twenty
	// steps said nothing at all in the report.
	var lines []string
	for i := 0; i < 320; i++ {
		lines = append(lines,
			sentinelLine(t, sentinel.Message{Kind: sentinel.KindStepStart, Name: "s"}),
			sentinelLine(t, sentinel.Message{Kind: sentinel.KindStepStop, Status: "passed"}))
	}
	p := pkg("m", test("TestManySteps", occ(model.StatusPassed, 1, lines...)))

	got, _ := Cases(p, p.Tests["TestManySteps"], Options{})
	w := got[0].Properties["qualflare.warnings"]
	if !strings.Contains(w, "steps") {
		t.Errorf("expected a dropped-steps warning, got %q", w)
	}
}

func TestWarnings_AnUndecodableSentinelIsReported(t *testing.T) {
	p := pkg("m", test("TestThing", occ(model.StatusPassed, 1, "@@QF1@@bogus@@00000000@@")))
	got, _ := Cases(p, p.Tests["TestThing"], Options{})
	if got[0].Properties["qualflare.warnings"] == "" {
		t.Error("a line that looked like metadata but did not decode should be reported")
	}
}

func TestWarnings_ACleanCaseCarriesNone(t *testing.T) {
	p := pkg("m", test("TestThing", occ(model.StatusPassed, 1, "    x_test.go:1: ordinary output")))
	got, _ := Cases(p, p.Tests["TestThing"], Options{})
	if _, ok := got[0].Properties["qualflare.warnings"]; ok {
		t.Error("a clean case must carry no warnings")
	}
}

func TestDiagnostics_StreamAnomaliesReachTheReport(t *testing.T) {
	// On a fresh Go release a non-zero count here is the drift alarm saying a
	// new action or output shape has shipped.
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	got := Collect(Input{
		Run: run(p), Cfg: config.Config{Framework: "golang"},
		Stats: stream.Stats{UnparsedLines: 3, OversizedLines: 1, UnknownActions: map[string]int{"quantum": 2}},
	})
	if got.Properties["qualflare.unparsedLines"] != "3" {
		t.Errorf("unparsedLines = %q", got.Properties["qualflare.unparsedLines"])
	}
	if got.Properties["qualflare.oversizedLines"] != "1" {
		t.Errorf("oversizedLines = %q", got.Properties["qualflare.oversizedLines"])
	}
	if got.Properties["qualflare.unknownAction.quantum"] != "2" {
		t.Errorf("unknownAction = %q", got.Properties["qualflare.unknownAction.quantum"])
	}
}

func TestDiagnostics_ACleanRunAddsNothing(t *testing.T) {
	p := pkg("m", test("TestPasses", occ(model.StatusPassed, 1)))
	got := Collect(Input{Run: run(p), Cfg: config.Config{Framework: "golang"}, Stats: stream.Stats{}})
	for k := range got.Properties {
		if strings.HasPrefix(k, "qualflare.") {
			t.Errorf("a clean run should add no diagnostics, got %q", k)
		}
	}
}
