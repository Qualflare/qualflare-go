// Package gitdetect reads branch and commit from git, used only when CI
// variables are absent.
//
// Two short subprocesses, each guarded: a reporter must never be the reason a
// test run fails, and a shallow or detached checkout is normal in CI rather
// than an error worth surfacing.
package gitdetect

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type Info struct {
	Branch string
	Commit string
}

func Detect() Info {
	branch := run("rev-parse", "--abbrev-ref", "HEAD")
	// A detached HEAD reports the literal "HEAD", which is not a branch name --
	// and CI checkouts are routinely detached, so this is the common case
	// rather than an edge one.
	if branch == "HEAD" {
		branch = ""
	}
	return Info{Branch: branch, Commit: run("rev-parse", "HEAD")}
}

var runner = func(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return string(out), err
}

func run(args ...string) string {
	out, err := runner(args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// SetRunnerForTest replaces the git invocation for the duration of a test.
// Detection shells out, so without this a test asserts whatever branch the
// working copy happens to be on.
func SetRunnerForTest(t interface{ Cleanup(func()) }, fn func(args ...string) (string, error)) {
	prev := runner
	runner = fn
	t.Cleanup(func() { runner = prev })
}
