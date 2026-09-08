package cidetect

import "testing"

// The whole environment is cleared per test. Without that these pass locally
// and behave differently on Actions, where the real GITHUB_* variables are set.
var touched = []string{
	"GITHUB_ACTIONS", "GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_NUMBER",
	"GITHUB_RUN_ATTEMPT", "GITHUB_REF", "GITHUB_REF_NAME", "GITHUB_HEAD_REF", "GITHUB_SHA",
	"GITLAB_CI", "CI_PIPELINE_IID", "CI_PIPELINE_ID", "CI_PIPELINE_URL",
	"CI_MERGE_REQUEST_IID", "CI_COMMIT_REF_NAME", "CI_COMMIT_SHA",
	"CIRCLECI", "CIRCLE_BUILD_NUM", "CIRCLE_BUILD_URL", "CIRCLE_BRANCH",
	"CIRCLE_SHA1", "CIRCLE_WORKFLOW_ID",
	"JENKINS_URL", "BUILD_NUMBER", "BUILD_URL", "BUILD_TAG", "GIT_BRANCH", "GIT_COMMIT",
}

func clean(t *testing.T) {
	t.Helper()
	for _, name := range touched {
		t.Setenv(name, "")
	}
}

func TestNothingDetectedOutsideCI(t *testing.T) {
	clean(t)
	if got := Detect(); got.Provider != "" {
		t.Errorf("provider = %q, want empty", got.Provider)
	}
}

func TestGitHub_PushBuild(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "Qualflare/qualflare-go")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_RUN_NUMBER", "42")
	t.Setenv("GITHUB_SHA", "abc123")
	t.Setenv("GITHUB_REF_NAME", "main")

	got := Detect()
	if got.Provider != "github" || got.BuildNumber != "42" || got.Branch != "main" || got.Commit != "abc123" {
		t.Errorf("got %+v", got)
	}
	if got.RunURL != "https://github.com/Qualflare/qualflare-go/actions/runs/12345" {
		t.Errorf("RunURL = %q", got.RunURL)
	}
	if got.PRNumber != nil {
		t.Errorf("PRNumber = %v, want nil", *got.PRNumber)
	}
}

func TestGitHub_PullRequestUsesTheSourceBranchNotTheMergeRef(t *testing.T) {
	// GITHUB_REF_NAME on a PR is "7/merge", which is not a branch anyone
	// recognises -- and the server groups history by branch.
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF_NAME", "7/merge")
	t.Setenv("GITHUB_HEAD_REF", "feature/checkout")
	t.Setenv("GITHUB_REF", "refs/pull/7/merge")

	got := Detect()
	if got.Branch != "feature/checkout" {
		t.Errorf("branch = %q, want the source branch", got.Branch)
	}
	if got.PRNumber == nil || *got.PRNumber != 7 {
		t.Errorf("PRNumber = %v, want 7", got.PRNumber)
	}
}

func TestGitHub_MalformedPullRefDegrades(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF", "refs/pull/not-a-number/merge")
	if got := Detect(); got.PRNumber != nil {
		t.Errorf("PRNumber = %v, want nil", *got.PRNumber)
	}
}

func TestGitHub_RunIDIncludesTheAttemptSoARerunIsItsOwnLaunch(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_RUN_ID", "12345")
	if got := Detect(); got.RunID != "gh-12345-1" {
		t.Errorf("RunID = %q, want gh-12345-1", got.RunID)
	}
	t.Setenv("GITHUB_RUN_ATTEMPT", "3")
	if got := Detect(); got.RunID != "gh-12345-3" {
		t.Errorf("RunID = %q, want gh-12345-3", got.RunID)
	}
}

func TestGitHub_NoRunURLInventedFromHalfTheInputs(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_RUN_ID", "12345")
	if got := Detect(); got.RunURL != "" {
		t.Errorf("RunURL = %q, want empty without a repository", got.RunURL)
	}
}

func TestGitHub_AnythingButTrueIsNotMatched(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "false")
	if got := Detect(); got.Provider != "" {
		t.Errorf("provider = %q", got.Provider)
	}
}

func TestGitLab(t *testing.T) {
	clean(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PIPELINE_IID", "17")
	t.Setenv("CI_PIPELINE_ID", "9001")
	t.Setenv("CI_COMMIT_REF_NAME", "main")
	t.Setenv("CI_MERGE_REQUEST_IID", "5")
	got := Detect()
	if got.Provider != "gitlab" || got.BuildNumber != "17" || got.RunID != "gl-9001" {
		t.Errorf("got %+v", got)
	}
	if got.PRNumber == nil || *got.PRNumber != 5 {
		t.Errorf("PRNumber = %v", got.PRNumber)
	}
}

func TestCircleCI(t *testing.T) {
	clean(t)
	t.Setenv("CIRCLECI", "true")
	t.Setenv("CIRCLE_BUILD_NUM", "88")
	t.Setenv("CIRCLE_BRANCH", "main")
	t.Setenv("CIRCLE_WORKFLOW_ID", "wf-1")
	got := Detect()
	if got.Provider != "circleci" || got.BuildNumber != "88" || got.RunID != "circle-wf-1" {
		t.Errorf("got %+v", got)
	}
}

func TestJenkins(t *testing.T) {
	// Matched on the presence of JENKINS_URL rather than a literal "true",
	// because it carries a URL.
	clean(t)
	t.Setenv("JENKINS_URL", "https://ci.example.com/")
	t.Setenv("BUILD_NUMBER", "31")
	t.Setenv("BUILD_TAG", "jenkins-x-31")
	t.Setenv("GIT_BRANCH", "origin/main")
	got := Detect()
	if got.Provider != "jenkins" || got.BuildNumber != "31" || got.RunID != "jenkins-jenkins-x-31" {
		t.Errorf("got %+v", got)
	}
}

func TestGitHubWinsWhenMoreThanOneLooksPresent(t *testing.T) {
	// Self-hosted runners can set both. Matching the first is deterministic;
	// matching neither loses the data.
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("JENKINS_URL", "https://ci.example.com/")
	if got := Detect(); got.Provider != "github" {
		t.Errorf("provider = %q, want github", got.Provider)
	}
}
