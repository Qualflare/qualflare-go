// Package cidetect identifies the CI provider from its environment variables.
//
// Only providers whose variables are unambiguous are matched. A wrong branch
// name is worse than no branch name, because the server groups history by it.
package cidetect

import (
	"os"
	"strconv"
	"strings"
)

// Info is what a CI provider reports about the current build.
type Info struct {
	Provider    string
	BuildNumber string
	RunURL      string
	PRNumber    *int
	Branch      string
	Commit      string
	RunID       string
}

func Detect() Info {
	switch {
	case os.Getenv("GITHUB_ACTIONS") == "true":
		return github()
	case os.Getenv("GITLAB_CI") == "true":
		return gitlab()
	case os.Getenv("CIRCLECI") == "true":
		return circleci()
	case os.Getenv("JENKINS_URL") != "":
		return jenkins()
	}
	return Info{}
}

func github() Info {
	repo, run := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
	info := Info{
		Provider:    "github",
		BuildNumber: os.Getenv("GITHUB_RUN_NUMBER"),
		Commit:      os.Getenv("GITHUB_SHA"),
		// GITHUB_HEAD_REF is set only on pull_request events and holds the
		// SOURCE branch; GITHUB_REF_NAME on a PR is "7/merge", the merge ref,
		// which is not a branch anyone recognises.
		Branch: first(os.Getenv("GITHUB_HEAD_REF"), os.Getenv("GITHUB_REF_NAME")),
	}
	if repo != "" && run != "" {
		info.RunURL = "https://github.com/" + repo + "/actions/runs/" + run
	}
	if run != "" {
		// The attempt is included so a re-run is its own launch rather than
		// merging into the previous one.
		attempt := os.Getenv("GITHUB_RUN_ATTEMPT")
		if attempt == "" {
			attempt = "1"
		}
		info.RunID = "gh-" + run + "-" + attempt
	}
	if ref := os.Getenv("GITHUB_REF"); strings.HasPrefix(ref, "refs/pull/") {
		if parts := strings.Split(ref, "/"); len(parts) > 2 {
			info.PRNumber = intOrNil(parts[2])
		}
	}
	return info
}

func gitlab() Info {
	info := Info{
		Provider:    "gitlab",
		BuildNumber: os.Getenv("CI_PIPELINE_IID"),
		RunURL:      os.Getenv("CI_PIPELINE_URL"),
		PRNumber:    intOrNil(os.Getenv("CI_MERGE_REQUEST_IID")),
		Branch:      os.Getenv("CI_COMMIT_REF_NAME"),
		Commit:      os.Getenv("CI_COMMIT_SHA"),
	}
	if id := os.Getenv("CI_PIPELINE_ID"); id != "" {
		info.RunID = "gl-" + id
	}
	return info
}

func circleci() Info {
	info := Info{
		Provider:    "circleci",
		BuildNumber: os.Getenv("CIRCLE_BUILD_NUM"),
		RunURL:      os.Getenv("CIRCLE_BUILD_URL"),
		Branch:      os.Getenv("CIRCLE_BRANCH"),
		Commit:      os.Getenv("CIRCLE_SHA1"),
	}
	if wf := os.Getenv("CIRCLE_WORKFLOW_ID"); wf != "" {
		info.RunID = "circle-" + wf
	}
	return info
}

func jenkins() Info {
	info := Info{
		Provider:    "jenkins",
		BuildNumber: os.Getenv("BUILD_NUMBER"),
		RunURL:      os.Getenv("BUILD_URL"),
		Branch:      os.Getenv("GIT_BRANCH"),
		Commit:      os.Getenv("GIT_COMMIT"),
	}
	if tag := os.Getenv("BUILD_TAG"); tag != "" {
		info.RunID = "jenkins-" + tag
	}
	return info
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func intOrNil(raw string) *int {
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &n
}
