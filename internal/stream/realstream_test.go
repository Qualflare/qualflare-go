package stream

import (
	"io"
	"os"
	"strings"
	"testing"
)

// These run against streams captured from real `go test -json` invocations
// rather than hand-written fixtures. A diff in a captured file is itself the
// alarm that a Go release changed the stream shape.

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func drain(t *testing.T, in string) ([]Event, Stats) {
	t.Helper()
	d := NewDecoder(strings.NewReader(in))
	var evs []Event
	for {
		ev, err := d.Next()
		if err == io.EOF {
			return evs, d.Stats()
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		evs = append(evs, ev)
	}
}

func TestReal_MixedRunParsesCleanly(t *testing.T) {
	// A clean stream must produce zero tolerated anomalies. A non-zero count
	// here on a new Go release is the drift alarm.
	evs, stats := drain(t, load(t, "real-mixed.jsonl"))
	if len(evs) == 0 {
		t.Fatal("no events decoded from a real run")
	}
	if stats.UnparsedLines != 0 || stats.OversizedLines != 0 || len(stats.UnknownActions) != 0 {
		t.Errorf("clean stream reported anomalies: %+v", stats)
	}

	var pkgs, subtests, terminal int
	seen := map[string]bool{}
	for _, e := range evs {
		seen[e.Action] = true
		if IsTerminal(e.Action) {
			terminal++
			if e.Test == "" {
				pkgs++
			} else if strings.Contains(e.Test, "/") {
				subtests++
			}
		}
	}
	if pkgs == 0 {
		t.Error("expected package-level terminal events (Test absent)")
	}
	if subtests == 0 {
		t.Error("expected subtest events containing '/'")
	}
	for _, a := range []string{"start", "run", "output", "pass"} {
		if !seen[a] {
			t.Errorf("expected a %q action in a real run", a)
		}
	}
	t.Logf("real run: %d events, %d terminal, %d package-level, %d subtests", len(evs), terminal, pkgs, subtests)
}

func TestReal_Go126BuildFailureJoinsThroughFailedBuild(t *testing.T) {
	evs, stats := drain(t, load(t, "buildfail-go126.jsonl"))
	if stats.UnparsedLines != 0 {
		t.Errorf("go1.26 emits build failures as JSON; got %d unparsed lines", stats.UnparsedLines)
	}

	var importPaths []string
	var failedBuild, failedPkg string
	for _, e := range evs {
		if e.ImportPath != "" {
			importPaths = append(importPaths, e.ImportPath)
		}
		if e.FailedBuild != "" {
			failedBuild, failedPkg = e.FailedBuild, e.Package
		}
	}
	if len(importPaths) == 0 {
		t.Fatal("expected build events carrying ImportPath")
	}
	if failedBuild == "" {
		t.Fatal("expected a fail event carrying FailedBuild")
	}
	if failedBuild == failedPkg {
		t.Fatalf("FailedBuild (%q) must not equal Package (%q)", failedBuild, failedPkg)
	}
	// The join that actually works.
	found := false
	for _, ip := range importPaths {
		if ip == failedBuild {
			found = true
		}
	}
	if !found {
		t.Errorf("FailedBuild %q matched no build event ImportPath %v", failedBuild, importPaths)
	}
}

func TestReal_Go121BuildFailureIsInvisibleInTheStream(t *testing.T) {
	// The finding that makes the exit-code backstop mandatory: on Go 1.21 a
	// broken build produces NO fail event at all. The stream is entirely green
	// while `go test` exits 1, and the compiler error goes to stderr.
	evs, _ := drain(t, load(t, "buildfail-go121.jsonl"))

	for _, e := range evs {
		if e.Action == "fail" {
			t.Fatalf("go1.21 unexpectedly reported a fail event: %+v", e)
		}
		if e.FailedBuild != "" {
			t.Fatalf("go1.21 unexpectedly carried FailedBuild: %+v", e)
		}
	}
	passes := 0
	for _, e := range evs {
		if e.Action == "pass" {
			passes++
		}
	}
	if passes == 0 {
		t.Fatal("expected the sibling package to pass")
	}

	// And the evidence lives only on stderr, which is why pipe mode cannot see it.
	stderr := load(t, "buildfail-go121.stderr")
	if !strings.Contains(stderr, "expected ';'") {
		t.Errorf("expected the compiler error on stderr, got %q", stderr)
	}
}

func TestReal_StreamsSurviveByteLevelCorruption(t *testing.T) {
	// Truncating a real stream at an arbitrary offset simulates a killed test
	// binary. The decoder must return what it read, never error out.
	full := load(t, "real-mixed.jsonl")
	for _, frac := range []int{3, 7, 13, 29, 61, 97} {
		cut := len(full) * frac / 100
		evs, stats := drain(t, full[:cut])
		if cut > 200 && len(evs) == 0 {
			t.Errorf("truncation at %d%% yielded no events at all", frac)
		}
		_ = stats
	}
}
