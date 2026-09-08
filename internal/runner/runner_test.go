package runner

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Run execs a real process, so these drive it with shell commands rather than a
// fake. The three properties that matter are the ones a pipe cannot give us:
// the child's exit code, its stderr, and its stdout streamed while it runs.

func drain(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	return string(b), err
}

func TestRun_CapturesStdoutThroughConsume(t *testing.T) {
	var got string
	res, err := Run([]string{"sh", "-c", "echo hello"}, func(r io.Reader) error {
		var e error
		got, e = drain(r)
		return e
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(got) != "hello" {
		t.Errorf("stdout = %q", got)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d", res.ExitCode)
	}
}

func TestRun_PropagatesTheExitCode(t *testing.T) {
	// The whole reason wrapper mode exists: a pipe reports the pipe's success,
	// not the child's, unless the user remembered `set -o pipefail`.
	res, err := Run([]string{"sh", "-c", "exit 3"}, func(r io.Reader) error {
		_, e := drain(r)
		return e
	})
	if err != nil {
		t.Fatalf("a non-zero exit is not an error to Run: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit = %d, want 3", res.ExitCode)
	}
}

func TestRun_CapturesStderr(t *testing.T) {
	// On Go before 1.24 a build failure appears ONLY here -- never in the JSON
	// stream -- so losing stderr means uploading a green launch for a broken
	// build.
	res, err := Run([]string{"sh", "-c", "echo 'compiler said no' 1>&2"}, func(r io.Reader) error {
		_, e := drain(r)
		return e
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Stderr, "compiler said no") {
		t.Errorf("stderr = %q", res.Stderr)
	}
}

func TestRun_StderrIsAlsoEchoedToTheUser(t *testing.T) {
	// Capturing it must not swallow it: the developer still needs to see the
	// compiler error in their terminal.
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_, err := Run([]string{"sh", "-c", "echo visible 1>&2"}, func(rd io.Reader) error {
		_, e := drain(rd)
		return e
	})
	w.Close()
	os.Stderr = old
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	echoed, _ := io.ReadAll(r)
	if !strings.Contains(string(echoed), "visible") {
		t.Errorf("stderr was captured but not echoed: %q", echoed)
	}
}

func TestRun_ConsumesWhileTheChildRuns(t *testing.T) {
	// `go test` on a large repo produces more output than a pipe buffer holds.
	// Draining only after Wait would deadlock, so this writes far more than a
	// pipe buffer (64 KiB on Linux) before exiting.
	var n int
	res, err := Run([]string{"sh", "-c", "for i in $(seq 1 20000); do echo 'a line of output padding padding'; done"},
		func(r io.Reader) error {
			b, e := io.ReadAll(r)
			n = len(b)
			return e
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n < 200_000 {
		t.Errorf("read %d bytes; a deadlock or truncation would show up here", n)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d", res.ExitCode)
	}
}

func TestRun_AMissingCommandIsAnError(t *testing.T) {
	if _, err := Run([]string{"definitely-not-a-real-binary-xyz"}, func(io.Reader) error { return nil }); err == nil {
		t.Error("expected an error for a command that does not exist")
	}
}

func TestRun_NothingToRunIsAnError(t *testing.T) {
	if _, err := Run(nil, func(io.Reader) error { return nil }); err == nil {
		t.Error("expected an error for an empty argv")
	}
}

func TestCapture_IsBounded(t *testing.T) {
	// A test that prints megabytes to stderr must not be held in memory whole.
	var c capture
	big := strings.Repeat("x", maxCapture*2)
	n, err := c.Write([]byte(big))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(big) {
		t.Errorf("Write reported %d, want %d -- it must claim the whole write", n, len(big))
	}
	if len(c.String()) != maxCapture {
		t.Errorf("captured %d bytes, want the cap %d", len(c.String()), maxCapture)
	}
}
