package version

import (
	"strings"
	"testing"
)

func TestString_PrefersTheLdflagsValue(t *testing.T) {
	prev := Version
	defer func() { Version = prev }()
	Version = "1.2.3"
	if got := String(); got != "1.2.3" {
		t.Errorf("got %q", got)
	}
}

func TestString_NeverReturnsEmpty(t *testing.T) {
	// A blank version in a report is worse than an honest "devel": it looks
	// like a field we failed to populate rather than a build with no tag.
	prev := Version
	defer func() { Version = prev }()
	Version = ""
	if got := String(); got == "" {
		t.Error("String() must never be empty")
	}
}

func TestFull_IncludesCommitAndDateWhenPresent(t *testing.T) {
	pv, pc, pd := Version, Commit, Date
	defer func() { Version, Commit, Date = pv, pc, pd }()

	Version, Commit, Date = "1.2.3", "abc1234", "2026-01-01"
	got := Full()
	for _, want := range []string{"1.2.3", "abc1234", "2026-01-01"} {
		if !strings.Contains(got, want) {
			t.Errorf("Full() = %q, missing %q", got, want)
		}
	}
}

func TestFull_OmitsTheParentheticalWithoutACommit(t *testing.T) {
	pv, pc := Version, Commit
	defer func() { Version, Commit = pv, pc }()
	Version, Commit = "1.2.3", ""
	if got := Full(); got != "1.2.3" {
		t.Errorf("got %q, want a bare version", got)
	}
}
