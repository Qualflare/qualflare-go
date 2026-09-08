package gitdetect

import (
	"errors"
	"os/exec"
	"strings"
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

// --- the real git invocation -------------------------------------------------

// Every test above replaces `runner` with a fake, which means the actual
// shell-out -- the only part that can be miswired against a real git -- was
// never executed by anything. These exercise it directly.
//
// They skip when git is absent rather than failing: a reporter must not require
// git, and the surrounding code is written to degrade quietly without it.

func gitAvailable(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
		return false
	}
	return true
}

func TestRealRunner_ReturnsGitsOutput(t *testing.T) {
	if !gitAvailable(t) {
		return
	}
	// The test binary runs inside this module's own checkout, so this resolves.
	out, err := runner("rev-parse", "HEAD")
	if err != nil {
		t.Skipf("not inside a git checkout: %v", err)
	}
	sha := strings.TrimSpace(out)
	if len(sha) != 40 {
		t.Errorf("rev-parse HEAD returned %q, want a 40-character sha", sha)
	}
	for _, r := range sha {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("rev-parse HEAD returned a non-hex sha: %q", sha)
		}
	}
}

func TestRealRunner_ReturnsAnErrorForABadSubcommand(t *testing.T) {
	// The error path matters more than the happy one: `run` swallows errors into
	// an empty string, so a runner that never reported failure would make every
	// detection silently succeed with garbage.
	if !gitAvailable(t) {
		return
	}
	if _, err := runner("definitely-not-a-git-subcommand"); err == nil {
		t.Error("expected an error from an invalid git subcommand")
	}
}

func TestRealDetect_DoesNotPanicAgainstRealGit(t *testing.T) {
	// Detect with the real runner in place. Whatever it returns depends on the
	// checkout, so this asserts only that it is safe to call -- which is the
	// contract the reporter relies on.
	if !gitAvailable(t) {
		return
	}
	got := Detect()
	if got.Commit != "" && len(got.Commit) != 40 {
		t.Errorf("commit = %q, want empty or a 40-character sha", got.Commit)
	}
	if got.Branch == "HEAD" {
		t.Error("a detached HEAD must never be reported as a branch named HEAD")
	}
}
