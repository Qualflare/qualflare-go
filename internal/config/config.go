// Package config resolves the reporter's configuration.
//
// Precedence, highest first: command-line flag -> QUALFLARE_* environment ->
// auto-detection (CI, then git) -> a hardcoded default. That is the same order
// the seven sibling reporters document, so one mental model covers every
// package, and the flag names track the pytest ini names 1:1 so the docs table
// is shared.
//
// There is deliberately NO token option. This tool makes no network calls, so
// it has no credential; `qf login` holds it instead.
package config

import (
	"os"
	"runtime"
	"strconv"

	"github.com/Qualflare/qualflare-go/internal/cidetect"
	"github.com/Qualflare/qualflare-go/internal/gitdetect"
)

const DefaultOutputDir = "qualflare-results"

// Config is the resolved configuration for one run.
type Config struct {
	OutputDir   string
	Environment string
	Language    string
	Framework   string
	Platform    string
	OS          string
	Milestone   *int
	Branch      string
	Commit      string
	RunID       string
	ShardIndex  *int
	Enabled     bool
	Repeat      string // collapse | split

	CIProvider    string
	CIBuildNumber string
	CIRunURL      string
	CIPRNumber    *int
}

// Flags carries the values supplied on the command line. An empty string means
// "not given", so it falls through to the next tier rather than overriding it
// with nothing.
type Flags struct {
	OutputDir   string
	Environment string
	Language    string
	Framework   string
	Platform    string
	Milestone   string
	Branch      string
	Commit      string
	RunID       string
	ShardIndex  string
	Enabled     string
	Repeat      string
}

// Resolve applies the precedence chain. newUUID supplies a fallback run id and
// is a parameter so the result is deterministic under test.
func Resolve(f Flags, newUUID func() string) Config {
	ci := cidetect.Detect()

	// git is consulted only when nothing better reported a branch, because it
	// shells out and a shallow checkout has nothing useful to say.
	var git gitdetect.Info
	if f.Branch == "" && env("QUALFLARE_BRANCH") == "" && ci.Branch == "" {
		git = gitdetect.Detect()
	}
	if f.Commit == "" && env("QUALFLARE_COMMIT") == "" && ci.Commit == "" && git.Commit == "" {
		git.Commit = gitdetect.Detect().Commit
	}

	c := Config{
		OutputDir:   pick(f.OutputDir, env("QUALFLARE_OUTPUT_DIR"), DefaultOutputDir),
		Environment: pick(f.Environment, env("QUALFLARE_ENVIRONMENT"), "development"),
		Language:    pick(f.Language, env("QUALFLARE_LANGUAGE"), "en-US"),
		Framework:   pick(f.Framework, env("QUALFLARE_FRAMEWORK"), "golang"),
		Platform:    pick(f.Platform, env("QUALFLARE_PLATFORM"), "api"),
		OS:          runtime.GOOS,
		// Branch and commit stay empty when nothing reports them: the wire
		// contract wants an explicit null, not a guess.
		Branch:  pick(f.Branch, env("QUALFLARE_BRANCH"), ci.Branch, git.Branch),
		Commit:  pick(f.Commit, env("QUALFLARE_COMMIT"), ci.Commit, git.Commit),
		Enabled: boolOr(pick(f.Enabled, env("QUALFLARE_ENABLED")), true),
		Repeat:  pick(f.Repeat, env("QUALFLARE_REPEAT"), "collapse"),

		CIProvider:    ci.Provider,
		CIBuildNumber: ci.BuildNumber,
		CIRunURL:      ci.RunURL,
		CIPRNumber:    ci.PRNumber,
	}

	// One run id per launch, shared by every shard so `qf collect` can group the
	// report files it finds. In CI it is derived from the build so shards agree
	// without coordinating; locally it is random per run.
	c.RunID = pick(f.RunID, env("QUALFLARE_RUN_ID"), ci.RunID)
	if c.RunID == "" {
		c.RunID = newUUID()
	}

	// A typo in a flag must not fail the run, so an unparseable value degrades
	// to "not set" rather than erroring.
	c.Milestone = intOrNil(pick(f.Milestone, env("QUALFLARE_MILESTONE")))
	c.ShardIndex = intOrNil(pick(f.ShardIndex, env("QUALFLARE_SHARD_INDEX")))

	return c
}

func env(name string) string { return os.Getenv(name) }

// pick returns the first non-empty value, so an empty flag or env var falls
// through instead of overriding a real value with nothing.
func pick(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func boolOr(raw string, def bool) bool {
	switch raw {
	case "":
		return def
	case "1", "true", "yes", "on", "TRUE", "True", "On", "Yes":
		return true
	default:
		return false
	}
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
