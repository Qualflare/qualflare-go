package gitdetect

import (
	"errors"
	"testing"
)

func TestDetect_NormalCheckout(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) {
		for _, a := range args {
			if a == "--abbrev-ref" {
				return "main\n", nil
			}
		}
		return "abc123\n", nil
	})
	if got := Detect(); got.Branch != "main" || got.Commit != "abc123" {
		t.Errorf("got %+v", got)
	}
}

func TestDetect_DetachedHeadReportsNoBranch(t *testing.T) {
	// `git rev-parse --abbrev-ref HEAD` returns the literal "HEAD" when
	// detached, which is not a branch name -- and a CI checkout is routinely
	// detached, so this is the common case rather than an edge one. The wire
	// contract wants an explicit null over a wrong name.
	SetRunnerForTest(t, func(args ...string) (string, error) {
		for _, a := range args {
			if a == "--abbrev-ref" {
				return "HEAD\n", nil
			}
		}
		return "abc123\n", nil
	})
	got := Detect()
	if got.Branch != "" {
		t.Errorf("branch = %q, want empty", got.Branch)
	}
	if got.Commit != "abc123" {
		t.Errorf("the commit is still usable: %q", got.Commit)
	}
}

func TestDetect_NoGitAvailableDegradesQuietly(t *testing.T) {
	// A reporter must never be the reason a test run fails.
	SetRunnerForTest(t, func(args ...string) (string, error) {
		return "", errors.New("git: not found")
	})
	if got := Detect(); got.Branch != "" || got.Commit != "" {
		t.Errorf("got %+v, want everything empty", got)
	}
}

func TestDetect_TrimsWhitespace(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) { return "  main  \n\n", nil })
	if got := Detect(); got.Branch != "main" {
		t.Errorf("branch = %q", got.Branch)
	}
}

func TestDetect_EmptyOutputIsNotABranch(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) { return "\n", nil })
	if got := Detect(); got.Branch != "" || got.Commit != "" {
		t.Errorf("got %+v", got)
	}
}
