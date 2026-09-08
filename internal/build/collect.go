package build

import (
	"strings"
	"time"

	"github.com/Qualflare/qualflare-go/internal/config"
	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/model"
	"github.com/Qualflare/qualflare-go/internal/stream"
	"github.com/Qualflare/qualflare-go/internal/textutil"
	"github.com/Qualflare/qualflare-go/internal/version"
	"github.com/Qualflare/qualflare-go/internal/wire"
)

// Input is everything the report is assembled from.
type Input struct {
	Run   *model.Run
	Cfg   config.Config
	Stats stream.Stats
	// Preamble is non-JSON text from the stream -- on a pre-1.24 toolchain this
	// is where a build failure's compiler error lives.
	Preamble string
	// ExitCode is the child's exit status in wrapper mode, and nil when the
	// reporter could not observe it (pipe and file modes).
	ExitCode *int
	Now      func() time.Time
}

// Collect assembles the report.
func Collect(in Input) wire.Collect {
	now := in.Now
	if now == nil {
		now = time.Now
	}

	c := wire.Collect{
		Framework:   in.Cfg.Framework,
		Platform:    in.Cfg.Platform,
		OS:          in.Cfg.OS,
		Environment: in.Cfg.Environment,
		Language:    in.Cfg.Language,
		Metadata: wire.Metadata{
			Version:   version.String(),
			Timestamp: now().UTC().Format(time.RFC3339),
			CLIName:   "qualflare-go",
			RunID:     in.Cfg.RunID,
		},
		Suites:        []wire.Suite{},
		Milestone:     in.Cfg.Milestone,
		CIProvider:    in.Cfg.CIProvider,
		CIBuildNumber: in.Cfg.CIBuildNumber,
		CIRunURL:      in.Cfg.CIRunURL,
		CIPRNumber:    in.Cfg.CIPRNumber,
	}
	// Value-or-null, always present: the server distinguishes "not reported"
	// from "absent", so these are pointers set only when something reported.
	if in.Cfg.Branch != "" {
		c.Branch = wire.StringPtr(in.Cfg.Branch)
	}
	if in.Cfg.Commit != "" {
		c.Commit = wire.StringPtr(in.Cfg.Commit)
	}

	opts := Options{Repeat: in.Cfg.Repeat, ShardIndex: in.Cfg.ShardIndex}
	for _, name := range in.Run.PackageOrder {
		if s, ok := Suite(in.Run, in.Run.Packages[name], opts); ok {
			if len(c.Suites) >= constants.MaxSuitesPerLaunch {
				break
			}
			c.Suites = append(c.Suites, s)
		}
	}

	applyBackstop(&c, in)
	return c
}

// Suite turns one package into one wire Suite. Returns false for a package that
// contributed nothing.
func Suite(run *model.Run, pkg *model.Package, opts Options) (wire.Suite, bool) {
	s := wire.NewSuite(pkg.Name, "golang")
	// The package's own reported elapsed, not the sum of its cases: under
	// -parallel the sum exceeds wall clock, and the package figure is the
	// honest "how long this took".
	s.Duration = pkg.ElapsedNanos

	for _, name := range pkg.TestOrder {
		if len(s.Cases) >= constants.MaxCasesPerSuite {
			break
		}
		if c, ok := Case(pkg, pkg.Tests[name], opts); ok {
			s.Cases = append(s.Cases, c)
		}
	}

	// A package that failed with nothing to blame it on -- a build failure, a
	// TestMain that exited, a panic before any test reported. Without this a
	// broken build uploads a GREEN launch.
	if pkg.Status == "fail" && !anyCaseFailed(s.Cases) {
		s.Cases = append(s.Cases, packageFailureCase(run, pkg))
	}

	// A package with no test files emits a package-level skip and nothing else.
	// Reporting it would add a meaningless green entry per package across a
	// monorepo and make the launch's skip count useless.
	if len(s.Cases) == 0 {
		return wire.Suite{}, false
	}
	return s, true
}

func anyCaseFailed(cases []wire.Case) bool {
	for _, c := range cases {
		if isFailure(c.Status) {
			return true
		}
	}
	return false
}

func packageFailureCase(run *model.Run, pkg *model.Package) wire.Case {
	name := pkg.Name + " [package failure]"
	text := strings.Join(pkg.Lines, "\n")

	// Build events key on ImportPath -- the test BINARY path -- which
	// deliberately does not equal Package. FailedBuild is the only link between
	// them; joining on Package equality matches nothing.
	if pkg.FailedBuild != "" {
		name = pkg.Name + " [build failure]"
		if out := run.BuildOutput[pkg.FailedBuild]; len(out) > 0 {
			text = strings.Join(out, "")
		}
	}

	return wire.Case{
		ID:        pkg.Name + ".[package]",
		Name:      name,
		ClassName: pkg.Name,
		Status:    model.StatusError,
		Error:     textutil.TruncateOr(text, constants.MaxCaseErrorRunes, "package-level failure not attributed to any test"),
	}
}

// applyBackstop is the last line of defence, and on any toolchain before Go
// 1.24 it is the ONLY one.
//
// Measured on Go 1.21 and 1.23: a package that fails to COMPILE produces no
// fail event at all. The stream contains one passing case and zero failures
// while `go test` exits 1, and the compiler error goes only to stderr. Every
// rule above is derived from events, so every rule above misses it.
//
// If the child exited non-zero and nothing in the report is non-passing, a
// failure is synthesised from whatever evidence exists. This catches failure
// modes nobody anticipated, including ones a future Go release introduces.
func applyBackstop(c *wire.Collect, in Input) {
	if in.ExitCode == nil || *in.ExitCode == 0 {
		return
	}
	for _, s := range c.Suites {
		if anyCaseFailed(s.Cases) {
			return
		}
	}

	evidence := strings.TrimSpace(in.Preamble)
	if evidence == "" {
		evidence = "go test exited " + itoa(*in.ExitCode) +
			" but no test reported a failure. On Go before 1.24 a build failure is absent from the JSON stream entirely."
	}

	s := wire.NewSuite("[unattributed]", "golang")
	s.Cases = append(s.Cases, wire.Case{
		ID:        "[unattributed].failure",
		Name:      "[unattributed failure]",
		ClassName: "[unattributed]",
		Status:    model.StatusError,
		Error:     textutil.Truncate(evidence, constants.MaxCaseErrorRunes),
	})
	c.Suites = append(c.Suites, s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
