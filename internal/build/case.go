// Package build assembles a Collect report from the run model.
package build

import (
	"fmt"
	"strings"
	"time"

	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/model"
	"github.com/Qualflare/qualflare-go/internal/replay"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
	"github.com/Qualflare/qualflare-go/internal/textutil"
	"github.com/Qualflare/qualflare-go/internal/wire"
)

// Options tunes assembly.
type Options struct {
	// Repeat is "collapse" (fold -count occurrences into attempts) or "split"
	// (emit each as its own case, for the -count=20 flake hunt where "the last
	// run passed" is a misleading summary).
	Repeat     string
	ShardIndex *int
}

// occurrenceView is one occurrence with its metadata already separated from its
// plain output.
type occurrenceView struct {
	occ      *model.Occurrence
	messages []sentinel.Message
	output   string
	warnings []string
}

// Cases turns one test into the cases it should contribute. Returns false when
// the test should not be reported at all.
//
// It returns a SLICE because of --repeat=split: `-count=20` is a flake hunt,
// and "the last run passed" is a misleading summary of one, so each occurrence
// becomes its own case. Every other mode returns exactly one.
func Cases(pkg *model.Package, t *model.Test, opts Options) ([]wire.Case, bool) {
	views := make([]occurrenceView, 0, len(t.Occurrences))
	for _, occ := range t.Occurrences {
		views = append(views, splitOutput(occ))
	}
	if len(views) == 0 {
		return nil, false
	}

	if !keepTest(pkg, t, views) {
		return nil, false
	}

	if opts.Repeat == "split" && len(views) > 1 {
		out := make([]wire.Case, 0, len(views))
		for i := range views {
			c := buildOne(pkg, t, views[i], opts)
			// Distinct ids, or the server merges what the user asked to see
			// separately -- which would defeat the entire point of the mode.
			c.ID = fmt.Sprintf("%s#%d", c.ID, i+1)
			c.Name = fmt.Sprintf("%s #%d", c.Name, i+1)
			c.Duration = views[i].occ.ElapsedNanos
			out = append(out, c)
		}
		return out, true
	}

	// The LAST occurrence decided the outcome, so it is the one the case
	// mirrors. api-service's migration 0243 makes "the final attempt mirrors
	// the case" an invariant, so status and duration must both come from here.
	final := views[len(views)-1]
	c := buildOne(pkg, t, final, opts)

	if len(views) > 1 {
		applyAttempts(&c, views)
	}
	return []wire.Case{c}, true
}

// buildOne renders a single occurrence as a case.
func buildOne(pkg *model.Package, t *model.Test, final occurrenceView, opts Options) wire.Case {
	c := wire.Case{
		ID:        pkg.Name + "." + t.Name,
		Name:      t.Name,
		ClassName: pkg.Name,
		Status:    final.occ.Status,
		Duration:  final.occ.ElapsedNanos,
	}
	if !final.occ.StartedAt.IsZero() {
		c.StartedAt = final.occ.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if isFailure(c.Status) {
		c.Error = textutil.Truncate(final.output, constants.MaxCaseErrorRunes)
	}

	// A parent kept only because it failed carries no duration of its own:
	// Go's Elapsed for a parent already includes every subtest, and those are
	// reported as their own cases.
	if pkg.HasChildren(t.Name) {
		c.Duration = 0
	}

	// Metadata comes from the final occurrence alone. Steps, labels and
	// attachments describe the attempt that decided the outcome; merging every
	// attempt's would double-count them.
	r := replay.Replay(final.messages)
	applyMetadata(&c, r)
	// Warnings were previously collected and discarded. A test that silently
	// lost twenty steps at the cap, or whose metadata line failed to decode,
	// said nothing at all in the report.
	applyWarnings(&c, append(append([]string{}, final.warnings...), r.Warnings...))

	if opts.ShardIndex != nil {
		c.ShardIndex = opts.ShardIndex
	}
	return c
}

// keepTest decides whether a test reaches the report.
//
// A parent with subtests is normally dropped: Go's Elapsed for it already
// includes its children, and every child is reported separately, so keeping it
// double-counts. Two exceptions matter, and qualflare-cli's parser honours
// neither -- it drops parents unconditionally:
//
//   - A parent that FAILED while no child did is a real parent-level assertion
//     or cleanup failure, and dropping it loses the only thing that went wrong.
//   - A parent carrying METADATA was deliberately annotated, and hoisting that
//     onto its children would duplicate labels and inflate counts.
func keepTest(pkg *model.Package, t *model.Test, views []occurrenceView) bool {
	if !pkg.HasChildren(t.Name) {
		return true
	}
	for _, v := range views {
		if len(v.messages) > 0 {
			return true
		}
	}
	if !isFailure(t.FinalStatus()) {
		return false
	}
	return !anyChildFailed(pkg, t.Name)
}

func anyChildFailed(pkg *model.Package, parent string) bool {
	prefix := parent + "/"
	for _, name := range pkg.TestOrder {
		if name == parent || !strings.HasPrefix(name, prefix) {
			continue
		}
		if isFailure(pkg.Tests[name].FinalStatus()) {
			return true
		}
	}
	return false
}

func isFailure(status string) bool {
	switch status {
	case model.StatusFailed, model.StatusError, model.StatusTimeout, model.StatusAborted:
		return true
	}
	return false
}

func applyWarnings(c *wire.Case, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	if c.Properties == nil {
		c.Properties = map[string]string{}
	}
	c.Properties["qualflare.warnings"] = textutil.Truncate(strings.Join(warnings, "; "), 2048)
}

func applyMetadata(c *wire.Case, r replay.Replayed) {
	c.Description = r.Description
	c.Priority = r.Priority
	c.Tags = r.Tags
	c.Labels = r.Labels
	c.Links = r.Links
	c.Steps = r.Steps
	c.Attachments = r.Attachments

	for _, p := range r.CaseParameters {
		if c.Properties == nil {
			c.Properties = map[string]string{}
		}
		if p.Masked || p.Value == nil {
			c.Properties[p.Name] = "••••••"
			continue
		}
		c.Properties[p.Name] = *p.Value
	}
}

// applyAttempts folds -count occurrences into one case.
//
// isFlaky is emitted only when the run ended green after an earlier failure.
// "Failed after retries" is failed, not flaky, and emitting isFlaky:false on
// every ordinary case would assert something we did not measure.
func applyAttempts(c *wire.Case, views []occurrenceView) {
	attempts := make([]wire.Attempt, 0, len(views))
	failedEarlier := false
	for i, v := range views {
		a := wire.Attempt{
			Attempt:  i + 1,
			Status:   v.occ.Status,
			Duration: wire.Int64Ptr(v.occ.ElapsedNanos),
		}
		if isFailure(v.occ.Status) {
			a.Message = textutil.Truncate(firstLine(v.output), constants.MaxAttemptMessageRunes)
			a.Trace = textutil.Truncate(v.output, constants.MaxAttemptTraceRunes)
			if i < len(views)-1 {
				failedEarlier = true
			}
		}
		attempts = append(attempts, a)
	}

	c.Attempts = wire.TrimAttempts(attempts)
	if len(c.Attempts) == 0 {
		return
	}
	c.RetryCount = wire.IntPtr(len(views) - 1)
	if !isFailure(c.Status) && failedEarlier {
		c.IsFlaky = wire.BoolPtr(true)
	}
}

// splitOutput separates a test's metadata from its plain output.
//
// A line that looks like a sentinel but does not decode is LEFT in the output
// and its problem recorded, never silently consumed -- a user must always be
// able to see what their test printed.
func splitOutput(occ *model.Occurrence) occurrenceView {
	v := occurrenceView{occ: occ}
	var plain []string
	for _, line := range occ.Lines {
		m, found, err := sentinel.Decode(line)
		switch {
		case found && err == nil:
			v.messages = append(v.messages, m)
		case found:
			v.warnings = append(v.warnings, err.Error())
			plain = append(plain, line)
		case isFraming(line):
			// "=== RUN", "=== PAUSE", "=== CONT", "=== NAME" are the runner
			// talking, not the test.
		default:
			plain = append(plain, line)
		}
	}
	v.output = strings.Join(plain, "\n")
	return v
}

func isFraming(line string) bool {
	trimmed := strings.TrimLeft(line, " \t\x16")
	for _, p := range []string{"=== RUN", "=== PAUSE", "=== CONT", "=== NAME"} {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
