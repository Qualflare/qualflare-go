// Package runner executes `go test -json` and tees its stream.
package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// Result is what the child process did.
type Result struct {
	ExitCode int
	// Stderr is the child's stderr. It matters because a pre-Go-1.24 build
	// failure appears ONLY here -- never in the JSON stream -- so a reporter
	// that cannot see it will happily upload a green launch for a broken build.
	Stderr string
}

// Run executes argv, streams its stdout through consume, and echoes its stderr
// to ours while capturing a copy.
//
// argv is the command as the user wrote it; -json is added by the caller.
func Run(argv []string, consume func(io.Reader) error) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("runner: nothing to run")
	}
	cmd := exec.Command(argv[0], argv[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr capture
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	// The stream is consumed while the child runs, not after: `go test` on a
	// large repo produces more output than a pipe buffer holds, and draining it
	// only at the end would deadlock.
	consumeErr := consume(stdout)
	_, _ = io.Copy(io.Discard, stdout) // drain anything consume left

	waitErr := cmd.Wait()
	res := Result{Stderr: stderr.String()}

	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
		res.ExitCode = 0
	case errors.As(waitErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		return res, waitErr
	}
	return res, consumeErr
}

// capture keeps a bounded copy of what it is written.
type capture struct{ buf []byte }

const maxCapture = 1 << 20

func (c *capture) Write(p []byte) (int, error) {
	if room := maxCapture - len(c.buf); room > 0 {
		if len(p) > room {
			c.buf = append(c.buf, p[:room]...)
		} else {
			c.buf = append(c.buf, p...)
		}
	}
	return len(p), nil
}

func (c *capture) String() string { return string(c.buf) }
