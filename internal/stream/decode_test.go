package stream

import (
	"io"
	"strings"
	"testing"
)

func decodeAll(t *testing.T, in string) ([]Event, *Decoder) {
	t.Helper()
	d := NewDecoder(strings.NewReader(in))
	var out []Event
	for {
		ev, err := d.Next()
		if err == io.EOF {
			return out, d
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, ev)
	}
}

func TestDecode_ReadsAWellFormedStream(t *testing.T) {
	in := `{"Action":"start","Package":"p"}
{"Action":"run","Package":"p","Test":"TestA"}
{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0.5}
`
	evs, d := decodeAll(t, in)
	if len(evs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(evs))
	}
	if evs[2].ElapsedNanos() != 500_000_000 {
		t.Errorf("Elapsed = %d ns, want 500000000", evs[2].ElapsedNanos())
	}
	if d.Stats().UnparsedLines != 0 {
		t.Errorf("clean stream should have no unparsed lines")
	}
}

func TestDecode_NonJSONIsRetainedNotDiscarded(t *testing.T) {
	// This is where a pre-1.24 toolchain reports a build failure. Silently
	// skipping it -- what the CLI parser does -- discards the only evidence
	// that anything was wrong.
	in := "# example.com/m/broken\nbroken_test.go:6:7: expected ';', found is\n" +
		`{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	evs, d := decodeAll(t, in)
	if len(evs) != 1 {
		t.Fatalf("expected the one real event, got %d", len(evs))
	}
	if d.Stats().UnparsedLines != 2 {
		t.Errorf("UnparsedLines = %d, want 2", d.Stats().UnparsedLines)
	}
	if !strings.Contains(d.Preamble(), "expected ';', found is") {
		t.Errorf("the compiler error must be retained, got %q", d.Preamble())
	}
}

func TestDecode_AnOversizedLineDoesNotCostTheReport(t *testing.T) {
	// The CLI parser's bufio.Scanner returns ErrTooLong here and loses every
	// event in the stream, including ones already read.
	huge := strings.Repeat("x", MaxLineBytes+4096)
	in := `{"Action":"run","Package":"p","Test":"TestA"}` + "\n" +
		huge + "\n" +
		`{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	evs, d := decodeAll(t, in)
	if len(evs) != 2 {
		t.Fatalf("events either side of the huge line must survive, got %d", len(evs))
	}
	if evs[1].Action != "pass" {
		t.Errorf("resynchronisation failed: second event = %q", evs[1].Action)
	}
	if d.Stats().OversizedLines != 1 {
		t.Errorf("OversizedLines = %d, want 1", d.Stats().OversizedLines)
	}
}

func TestDecode_MalformedJSONIsCountedNotFatal(t *testing.T) {
	in := `{"Action":"run","Package":"p","Test":"TestA"}` + "\n" +
		`{"Action":"pass"` + "\n" +
		`{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	evs, d := decodeAll(t, in)
	if len(evs) != 2 {
		t.Fatalf("expected 2 valid events, got %d", len(evs))
	}
	if d.Stats().UnparsedLines != 1 {
		t.Errorf("UnparsedLines = %d, want 1", d.Stats().UnparsedLines)
	}
}

func TestDecode_UnknownActionsAreCountedAndIgnored(t *testing.T) {
	// A future Go release adding an action must never change a test's status.
	in := `{"Action":"quantum","Package":"p","Test":"TestA"}` + "\n" +
		`{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	evs, d := decodeAll(t, in)
	if len(evs) != 1 || evs[0].Action != "pass" {
		t.Fatalf("unknown action must be dropped, got %+v", evs)
	}
	if d.Stats().UnknownActions["quantum"] != 1 {
		t.Errorf("UnknownActions = %v, want quantum:1", d.Stats().UnknownActions)
	}
}

func TestDecode_TruncatedFinalLineIsCounted(t *testing.T) {
	// A test binary killed mid-write leaves a partial line with no newline.
	in := `{"Action":"run","Package":"p","Test":"TestA"}` + "\n" + `{"Action":"pa`
	evs, d := decodeAll(t, in)
	if len(evs) != 1 {
		t.Fatalf("expected the one complete event, got %d", len(evs))
	}
	if d.Stats().UnparsedLines != 1 {
		t.Errorf("the partial tail must be counted, got %d", d.Stats().UnparsedLines)
	}
}

func TestDecode_BuildEventsCarryImportPathAndNoPackage(t *testing.T) {
	// Measured on Go 1.26.3: the ImportPath is the test BINARY path, and the
	// fail event joins to it through FailedBuild -- not through Package.
	in := `{"ImportPath":"m/broken.test","Action":"build-fail"}` + "\n" +
		`{"Action":"fail","Package":"m/broken","Elapsed":0,"FailedBuild":"m/broken.test"}` + "\n"
	evs, _ := decodeAll(t, in)
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	if evs[0].ImportPath != "m/broken.test" || evs[0].Package != "" {
		t.Errorf("build event = %+v; want ImportPath set and Package empty", evs[0])
	}
	if evs[1].FailedBuild != "m/broken.test" || evs[1].Package != "m/broken" {
		t.Errorf("fail event = %+v", evs[1])
	}
	if evs[1].FailedBuild == evs[1].Package {
		t.Error("FailedBuild must not equal Package -- a Package join would match nothing")
	}
}

func TestDecode_AttrAndArtifactsAreKnown(t *testing.T) {
	in := `{"Action":"attr","Package":"p","Test":"TestA","Key":"team","Value":"platform"}` + "\n" +
		`{"Action":"artifacts","Package":"p","Test":"TestA","Path":"/tmp/x"}` + "\n"
	evs, d := decodeAll(t, in)
	if len(evs) != 2 {
		t.Fatalf("expected both events, got %d (unknown=%v)", len(evs), d.Stats().UnknownActions)
	}
	if evs[0].Key != "team" || evs[0].Value != "platform" || evs[1].Path != "/tmp/x" {
		t.Errorf("attr/artifacts fields did not decode: %+v %+v", evs[0], evs[1])
	}
}

func TestIsTerminal(t *testing.T) {
	for _, a := range []string{"pass", "fail", "skip", "bench"} {
		if !IsTerminal(a) {
			t.Errorf("%q should be terminal", a)
		}
	}
	for _, a := range []string{"run", "output", "pause", "cont", "start", "attr", "build-fail"} {
		if IsTerminal(a) {
			t.Errorf("%q must not be terminal", a)
		}
	}
}

func TestElapsedNanos_AbsentIsZeroNotAnError(t *testing.T) {
	if (Event{}).ElapsedNanos() != 0 {
		t.Error("absent Elapsed should read as 0")
	}
	e := 0.0
	if (Event{Elapsed: &e}).ElapsedNanos() != 0 {
		t.Error("explicit zero Elapsed should read as 0")
	}
}
