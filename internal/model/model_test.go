package model

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/stream"
)

// Every fixture here is a stream captured from a real `go test -json` run
// against test/integration/fixtures/awkward. Hand-written streams prove only
// that the code agrees with my idea of Go's output.

func build(t *testing.T, name string) *Run {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()

	b := NewBuilder()
	d := stream.NewDecoder(f)
	for {
		ev, err := d.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		b.Add(ev)
	}
	return b.Finish()
}

func pkgBySuffix(t *testing.T, r *Run, suffix string) *Package {
	t.Helper()
	for _, name := range r.PackageOrder {
		if strings.HasSuffix(name, suffix) {
			return r.Packages[name]
		}
	}
	t.Fatalf("no package ending %q; have %v", suffix, r.PackageOrder)
	return nil
}

func statusOf(t *testing.T, p *Package, test string) string {
	t.Helper()
	tst, ok := p.Tests[test]
	if !ok {
		t.Fatalf("no test %q in %s; have %v", test, p.Name, p.TestOrder)
	}
	return tst.FinalStatus()
}

func TestMixed_BasicStatuses(t *testing.T) {
	p := pkgBySuffix(t, build(t, "mixed.jsonl"), "pkg_pass")
	if got := statusOf(t, p, "TestPasses"); got != StatusPassed {
		t.Errorf("TestPasses = %s", got)
	}
	if got := statusOf(t, p, "TestSkippedUpFront"); got != StatusSkipped {
		t.Errorf("TestSkippedUpFront = %s", got)
	}
	if got := statusOf(t, p, "TestSkippedMidway"); got != StatusSkipped {
		t.Errorf("TestSkippedMidway = %s", got)
	}
	if got := statusOf(t, p, "ExampleThing"); got != StatusPassed {
		t.Errorf("ExampleThing = %s -- an example is an ordinary test", got)
	}
}

func TestMixed_AParentThatFailsWhileSubtestsPassIsRecorded(t *testing.T) {
	// qualflare-cli's parser drops this parent unconditionally, losing the only
	// thing that actually failed. The model must keep it; the drop decision is
	// downstream and conditional.
	p := pkgBySuffix(t, build(t, "mixed.jsonl"), "pkg_subtests")

	if got := statusOf(t, p, "TestParentFailsAllSubtestsPass"); got != StatusFailed {
		t.Errorf("the parent must be recorded as failed, got %s", got)
	}
	for _, sub := range []string{"TestParentFailsAllSubtestsPass/sub_one", "TestParentFailsAllSubtestsPass/sub_two"} {
		if got := statusOf(t, p, sub); got != StatusPassed {
			t.Errorf("%s = %s, want passed", sub, got)
		}
	}
	if !p.HasChildren("TestParentFailsAllSubtestsPass") {
		t.Error("HasChildren should see the subtests")
	}
}

func TestMixed_DeepNestingIsPreservedAtEveryLevel(t *testing.T) {
	p := pkgBySuffix(t, build(t, "mixed.jsonl"), "pkg_subtests")
	for _, name := range []string{"TestDeepNesting", "TestDeepNesting/a", "TestDeepNesting/a/b", "TestDeepNesting/a/b/c"} {
		if _, ok := p.Tests[name]; !ok {
			t.Errorf("missing %q; have %v", name, p.TestOrder)
		}
	}
	if p.HasChildren("TestDeepNesting/a/b/c") {
		t.Error("the deepest subtest has no children")
	}
}

func TestMixed_SubtestNameSpacesBecomeUnderscores(t *testing.T) {
	// Go rewrites spaces in subtest names. We record Go's spelling, because it
	// is what `-run` accepts and what the user will search for.
	p := pkgBySuffix(t, build(t, "mixed.jsonl"), "pkg_subtests")
	if _, ok := p.Tests["TestSubtestNameWithSpaces/name_with_spaces"]; !ok {
		t.Errorf("expected Go's underscored spelling; have %v", p.TestOrder)
	}
}

func TestMixed_APackageWithNoTestFilesYieldsNoTests(t *testing.T) {
	p := pkgBySuffix(t, build(t, "mixed.jsonl"), "pkg_notests")
	if len(p.TestOrder) != 0 {
		t.Errorf("expected no tests, got %v", p.TestOrder)
	}
	if p.Status != "skip" {
		t.Errorf("package status = %q, want skip", p.Status)
	}
}

func TestPanic_APanicInATestBodyIsAnOrdinaryFailure(t *testing.T) {
	// Measured, and not what I first assumed: Go recovers a panic in a test
	// body and repanics it, emitting proper `fail` events for BOTH the subtest
	// and its parent. So this is a normal failure, not a non-terminal event --
	// the error/aborted path below is only for panics that escape the framework
	// entirely.
	p := pkgBySuffix(t, build(t, "panic.jsonl"), "pkg_panic")

	if got := statusOf(t, p, "TestPanickingSubtest/first_ok"); got != StatusPassed {
		t.Errorf("the sibling that finished should keep its status, got %s", got)
	}
	if got := statusOf(t, p, "TestPanickingSubtest/second_panics"); got != StatusFailed {
		t.Errorf("the panicking subtest = %s, want failed", got)
	}
	if got := statusOf(t, p, "TestPanickingSubtest"); got != StatusFailed {
		t.Errorf("the parent = %s, want failed", got)
	}
	// A subtest that never started must not be invented.
	if _, ok := p.Tests["TestPanickingSubtest/third_never_runs"]; ok {
		t.Error("a subtest that never ran must not appear")
	}
}

func TestGoroutinePanic_NoTerminalEventBecomesError(t *testing.T) {
	// The genuine non-terminal path: a panic on a goroutine the framework does
	// not own kills the process, so the test emits `run` and nothing else. The
	// stream carries zero terminal events for it, and the only signal is the
	// package-level fail plus the panic text.
	p := pkgBySuffix(t, build(t, "goroutine_panic.jsonl"), "pkg_goroutine_panic")
	got := statusOf(t, p, "TestGoroutinePanic")
	if got == StatusPassed {
		t.Fatal("a test killed by a goroutine panic must never be reported as passed")
	}
	if got != StatusError {
		t.Errorf("TestGoroutinePanic = %s, want error", got)
	}
}

func TestTimeout_IsTimeoutNotError(t *testing.T) {
	// The wire carries a real timeout status; folding it into error is pure
	// loss. The banner is usually attributed to the package, which is why
	// classification consults the package's output too.
	p := pkgBySuffix(t, build(t, "timeout.jsonl"), "pkg_timeout")
	if got := statusOf(t, p, "TestHangsForever"); got != StatusTimeout {
		t.Errorf("TestHangsForever = %s, want timeout", got)
	}
}

func TestTimeoutParallel_EveryHungTestIsATimeout(t *testing.T) {
	// Measured: with TestHangA, B and C all sleeping when the deadline fires,
	// the runtime prints ONE banner and test2json attributes it to TestHangC
	// alone. A and B carry no panic text whatsoever. Classifying on a test's
	// own output would report three identically-hung tests as one timeout and
	// two aborteds, which is why the check is package-wide.
	p := pkgBySuffix(t, build(t, "timeout_parallel.jsonl"), "pkg_timeout_parallel")
	for _, name := range []string{"TestHangA", "TestHangB", "TestHangC"} {
		if got := statusOf(t, p, name); got != StatusTimeout {
			t.Errorf("%s = %s, want timeout", name, got)
		}
	}
}

func TestExit_AnAbruptExitIsNotReportedAsPassed(t *testing.T) {
	// os.Exit(3) means test2json never sees a final PASS/FAIL, so no
	// package-level event is emitted at all. The one thing that must never
	// happen is reporting it green.
	p := pkgBySuffix(t, build(t, "exit.jsonl"), "pkg_exit")
	got := statusOf(t, p, "TestExitsAbruptly")
	if got == StatusPassed {
		t.Fatal("a test that called os.Exit(3) must never be reported as passed")
	}
	if got != StatusAborted && got != StatusError {
		t.Errorf("TestExitsAbruptly = %s, want aborted or error", got)
	}
}

func TestParallel_EachSubtestKeepsItsOwnPayloadIntact(t *testing.T) {
	// THE test for the (package, test) buffer key. Eight parallel tests each
	// log a 100 KB payload; test2json delivers them interleaved and in ~1 KB
	// pieces. A package-keyed buffer splices them into corrupt lines.
	p := pkgBySuffix(t, build(t, "parallel.jsonl"), "pkg_parallel")

	for _, w := range []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8"} {
		name := "TestParallelBigOutput/" + w
		tst, ok := p.Tests[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		text := strings.Join(tst.Occurrences[0].Lines, "\n")
		begin, end := "BEGIN-"+w+"-", "-END-"+w
		if !strings.Contains(text, begin) || !strings.Contains(text, end) {
			t.Errorf("%s lost its payload markers", name)
			continue
		}
		// No other worker's marker may appear in this test's output.
		for _, other := range []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8"} {
			if other == w {
				continue
			}
			if strings.Contains(text, "BEGIN-"+other+"-") {
				t.Errorf("%s contains %s's payload -- buffers spliced", name, other)
			}
		}
		if body := strings.Repeat(w, 20000); !strings.Contains(text, body) {
			t.Errorf("%s payload truncated: %d bytes captured", name, len(text))
		}
	}
}

func TestCount3_EachIterationIsItsOwnOccurrence(t *testing.T) {
	// -count re-runs the whole list, so occurrences are NOT adjacent in the
	// stream: A B A B A B. Keying on (package, test) alone keeps only the last
	// and silently discards the iteration-1 failure.
	p := pkgBySuffix(t, build(t, "count3.jsonl"), "pkg_count")

	tst, ok := p.Tests["TestFailsOnlyOnTheFirstIteration"]
	if !ok {
		t.Fatalf("missing test; have %v", p.TestOrder)
	}
	if len(tst.Occurrences) != 3 {
		t.Fatalf("expected 3 occurrences, got %d", len(tst.Occurrences))
	}
	got := []string{tst.Occurrences[0].Status, tst.Occurrences[1].Status, tst.Occurrences[2].Status}
	want := []string{StatusFailed, StatusPassed, StatusPassed}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("occurrence statuses = %v, want %v", got, want)
		}
	}
	if tst.FinalStatus() != StatusPassed {
		t.Errorf("FinalStatus = %s, want passed", tst.FinalStatus())
	}

	if other := p.Tests["TestAlwaysPasses"]; len(other.Occurrences) != 3 {
		t.Errorf("the always-passing test should also have 3 occurrences, got %d", len(other.Occurrences))
	}
}

func TestCount3_StartedAtIsRecordedPerOccurrence(t *testing.T) {
	p := pkgBySuffix(t, build(t, "count3.jsonl"), "pkg_count")
	tst := p.Tests["TestFailsOnlyOnTheFirstIteration"]
	for i, occ := range tst.Occurrences {
		if occ.StartedAt.IsZero() {
			t.Errorf("occurrence %d has no StartedAt", i+1)
		}
	}
	if !tst.Occurrences[1].StartedAt.After(tst.Occurrences[0].StartedAt) {
		t.Error("later occurrences should start later")
	}
}
