package config

import (
	"testing"

	"github.com/Qualflare/qualflare-go/internal/gitdetect"
)

var ciVars = []string{
	"QUALFLARE_OUTPUT_DIR", "QUALFLARE_ENVIRONMENT", "QUALFLARE_LANGUAGE",
	"QUALFLARE_FRAMEWORK", "QUALFLARE_PLATFORM", "QUALFLARE_MILESTONE",
	"QUALFLARE_BRANCH", "QUALFLARE_COMMIT", "QUALFLARE_RUN_ID",
	"QUALFLARE_SHARD_INDEX", "QUALFLARE_ENABLED", "QUALFLARE_REPEAT",
	"GITHUB_ACTIONS", "GITHUB_REF_NAME", "GITHUB_SHA", "GITHUB_RUN_ID",
	"GITHUB_REPOSITORY", "GITHUB_REF", "GITLAB_CI", "CIRCLECI", "JENKINS_URL",
}

// clean clears the ambient environment AND pins git detection. Without both,
// these pass locally and behave differently on Actions, where GITHUB_REF_NAME
// is set and quietly wins over the default they assert.
func clean(t *testing.T) {
	t.Helper()
	for _, name := range ciVars {
		t.Setenv(name, "")
	}
	gitdetect.SetRunnerForTest(t, func(args ...string) (string, error) { return "", errNoGit })
}

var errNoGit = &noGitError{}

type noGitError struct{}

func (*noGitError) Error() string { return "no git" }

func fixedUUID() string { return "fixed-uuid" }

func TestDefaults(t *testing.T) {
	clean(t)
	c := Resolve(Flags{}, fixedUUID)
	if c.OutputDir != DefaultOutputDir {
		t.Errorf("OutputDir = %q", c.OutputDir)
	}
	if c.Environment != "development" || c.Language != "en-US" || c.Platform != "api" {
		t.Errorf("got %+v", c)
	}
	if c.Framework != "golang" {
		t.Errorf("Framework = %q, want golang -- it drives the per-tool logo", c.Framework)
	}
	if !c.Enabled {
		t.Error("enabled should default to true")
	}
	if c.Repeat != "collapse" {
		t.Errorf("Repeat = %q, want collapse", c.Repeat)
	}
}

func TestBranchAndCommitStayEmptyWhenNothingReportsThem(t *testing.T) {
	// The wire contract wants an explicit null, not a guess.
	clean(t)
	c := Resolve(Flags{}, fixedUUID)
	if c.Branch != "" || c.Commit != "" {
		t.Errorf("branch=%q commit=%q, want both empty", c.Branch, c.Commit)
	}
}

func TestRunIDIsAlwaysProduced(t *testing.T) {
	// Shards group on it, so it can never be empty.
	clean(t)
	if c := Resolve(Flags{}, fixedUUID); c.RunID == "" {
		t.Error("RunID must never be empty")
	}
}

func TestEnvBeatsTheDefault(t *testing.T) {
	clean(t)
	t.Setenv("QUALFLARE_OUTPUT_DIR", "from-env")
	if c := Resolve(Flags{}, fixedUUID); c.OutputDir != "from-env" {
		t.Errorf("OutputDir = %q", c.OutputDir)
	}
}

func TestFlagBeatsEnv(t *testing.T) {
	clean(t)
	t.Setenv("QUALFLARE_OUTPUT_DIR", "from-env")
	if c := Resolve(Flags{OutputDir: "from-flag"}, fixedUUID); c.OutputDir != "from-flag" {
		t.Errorf("OutputDir = %q, want from-flag", c.OutputDir)
	}
}

func TestAnEmptyValueFallsThroughRatherThanWinning(t *testing.T) {
	// An unset flag must not override a real env value with nothing.
	clean(t)
	t.Setenv("QUALFLARE_ENVIRONMENT", "staging")
	if c := Resolve(Flags{Environment: ""}, fixedUUID); c.Environment != "staging" {
		t.Errorf("Environment = %q, want staging", c.Environment)
	}
}

func TestCIDetectionFillsBranchWhenNothingElseDoes(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF_NAME", "main")
	t.Setenv("GITHUB_SHA", "abc123")
	t.Setenv("GITHUB_RUN_ID", "99")
	c := Resolve(Flags{}, fixedUUID)
	if c.Branch != "main" || c.Commit != "abc123" {
		t.Errorf("branch=%q commit=%q", c.Branch, c.Commit)
	}
	if c.CIProvider != "github" {
		t.Errorf("CIProvider = %q", c.CIProvider)
	}
	if c.RunID != "gh-99-1" {
		t.Errorf("RunID = %q, want the CI-derived one so shards agree", c.RunID)
	}
}

func TestExplicitConfigurationBeatsCIDetection(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF_NAME", "from-ci")
	if c := Resolve(Flags{Branch: "from-flag"}, fixedUUID); c.Branch != "from-flag" {
		t.Errorf("Branch = %q", c.Branch)
	}
	t.Setenv("QUALFLARE_BRANCH", "from-env")
	if c := Resolve(Flags{}, fixedUUID); c.Branch != "from-env" {
		t.Errorf("Branch = %q, want from-env", c.Branch)
	}
}

func TestGitSuppliesBranchWhenCIIsAbsent(t *testing.T) {
	clean(t)
	gitdetect.SetRunnerForTest(t, func(args ...string) (string, error) {
		for _, a := range args {
			if a == "--abbrev-ref" {
				return "feature/x\n", nil
			}
		}
		return "deadbeef\n", nil
	})
	c := Resolve(Flags{}, fixedUUID)
	if c.Branch != "feature/x" || c.Commit != "deadbeef" {
		t.Errorf("branch=%q commit=%q", c.Branch, c.Commit)
	}
}

func TestMilestone(t *testing.T) {
	clean(t)
	t.Setenv("QUALFLARE_MILESTONE", "7")
	if c := Resolve(Flags{}, fixedUUID); c.Milestone == nil || *c.Milestone != 7 {
		t.Errorf("Milestone = %v", c.Milestone)
	}
	// A typo must not fail the run.
	t.Setenv("QUALFLARE_MILESTONE", "next-release")
	if c := Resolve(Flags{}, fixedUUID); c.Milestone != nil {
		t.Errorf("Milestone = %v, want nil", *c.Milestone)
	}
}

func TestShardIndexZeroIsRealAndPreserved(t *testing.T) {
	// Shard 0 is a real shard; nil and 0 must stay distinguishable.
	clean(t)
	t.Setenv("QUALFLARE_SHARD_INDEX", "0")
	c := Resolve(Flags{}, fixedUUID)
	if c.ShardIndex == nil {
		t.Fatal("shard 0 must not read as unset")
	}
	if *c.ShardIndex != 0 {
		t.Errorf("ShardIndex = %d", *c.ShardIndex)
	}
}

func TestEnabledSpellings(t *testing.T) {
	clean(t)
	for _, on := range []string{"1", "true", "yes", "on"} {
		t.Setenv("QUALFLARE_ENABLED", on)
		if !Resolve(Flags{}, fixedUUID).Enabled {
			t.Errorf("%q should enable", on)
		}
	}
	for _, off := range []string{"0", "false", "no", "off", "maybe"} {
		t.Setenv("QUALFLARE_ENABLED", off)
		if Resolve(Flags{}, fixedUUID).Enabled {
			t.Errorf("%q should disable", off)
		}
	}
}
