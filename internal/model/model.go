// Package model turns a `go test -json` event stream into a faithful record of
// what ran.
//
// It records and classifies; it does not decide what reaches the report. The
// parent-test drop rule, for instance, depends on whether a parent carried
// metadata, which is not known until the sentinel messages have been replayed --
// so that decision belongs downstream, and this package keeps everything.
//
// The one judgement it does make is status, because that depends on the event
// stream and nothing else.
package model

import (
	"strings"
	"time"

	"github.com/Qualflare/qualflare-go/internal/stream"
)

// Wire statuses. Go's vocabulary is narrower than the contract's seven, but
// timeout and aborted are both genuinely reachable and worth the fidelity --
// qualflare-cli's parser folds every one of these into "error".
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
	StatusError   = "error"
	StatusTimeout = "timeout"
	StatusAborted = "aborted"
)

// Occurrence is one execution of one test. There is more than one only under
// `-count=N`.
type Occurrence struct {
	Status       string
	ElapsedNanos int64
	StartedAt    time.Time
	Lines        []string
	terminal     bool
}

// Test is every execution of one test name within one package.
type Test struct {
	Name        string
	Occurrences []*Occurrence
}

// Package is one Go package's worth of results.
type Package struct {
	Name         string
	Status       string
	ElapsedNanos int64
	Lines        []string
	// FailedBuild is the test BINARY import path, which does not equal Name.
	FailedBuild string
	Tests       map[string]*Test
	TestOrder   []string
}

// Run is the whole invocation.
type Run struct {
	Packages     map[string]*Package
	PackageOrder []string
	// BuildOutput is keyed by build-event ImportPath, which is what
	// Package.FailedBuild points at. Present only on Go 1.24+.
	BuildOutput map[string][]string
}

// Builder accumulates events. Add is order-tolerant beyond the one guarantee
// the toolchain makes: a test's own events are ordered relative to each other.
type Builder struct {
	joiner *stream.Joiner
	run    *Run
	// open tracks the occurrence currently receiving output for a test, so a
	// second `run` under -count opens a new one rather than overwriting.
	open map[string]*Occurrence
}

func NewBuilder() *Builder {
	return &Builder{
		joiner: stream.NewJoiner(),
		run: &Run{
			Packages:    map[string]*Package{},
			BuildOutput: map[string][]string{},
		},
		open: map[string]*Occurrence{},
	}
}

func (b *Builder) pkg(name string) *Package {
	p, ok := b.run.Packages[name]
	if !ok {
		p = &Package{Name: name, Tests: map[string]*Test{}}
		b.run.Packages[name] = p
		b.run.PackageOrder = append(b.run.PackageOrder, name)
	}
	return p
}

func openKey(pkg, test string) string { return pkg + "\x00" + test }

func (b *Builder) Add(ev stream.Event) {
	// Build events carry ImportPath and no Package at all.
	if ev.ImportPath != "" {
		if ev.Output != "" {
			b.run.BuildOutput[ev.ImportPath] = append(b.run.BuildOutput[ev.ImportPath], ev.Output)
		}
		return
	}
	if ev.Package == "" {
		return
	}
	p := b.pkg(ev.Package)

	if ev.Output != "" {
		for _, line := range b.joiner.Add(ev.Package, ev.Test, ev.Output) {
			b.appendLine(p, ev.Test, line)
		}
		return
	}

	if ev.Test == "" {
		if stream.IsTerminal(ev.Action) {
			p.Status = ev.Action
			p.ElapsedNanos = ev.ElapsedNanos()
			p.FailedBuild = ev.FailedBuild
		}
		return
	}

	t, ok := p.Tests[ev.Test]
	if !ok {
		t = &Test{Name: ev.Test}
		p.Tests[ev.Test] = t
		p.TestOrder = append(p.TestOrder, ev.Test)
	}

	switch {
	case ev.Action == "run":
		// A `run` for a test that already finished is a further -count
		// iteration, not a repeat of the same one. Overwriting here -- which is
		// what a map keyed on (package, test) alone does -- silently discards a
		// first-iteration failure.
		occ := &Occurrence{}
		if ev.Time != nil {
			occ.StartedAt = *ev.Time
		}
		t.Occurrences = append(t.Occurrences, occ)
		b.open[openKey(ev.Package, ev.Test)] = occ

	case stream.IsTerminal(ev.Action):
		occ := b.currentOccurrence(p, t, ev.Package, ev.Test)
		occ.terminal = true
		occ.ElapsedNanos = ev.ElapsedNanos()
		switch ev.Action {
		case "pass", "bench":
			occ.Status = StatusPassed
		case "fail":
			occ.Status = StatusFailed
		case "skip":
			occ.Status = StatusSkipped
		}
	}
}

// currentOccurrence returns the occurrence a terminal event belongs to,
// synthesising one when the `run` event was never seen (a truncated stream).
func (b *Builder) currentOccurrence(p *Package, t *Test, pkg, test string) *Occurrence {
	if occ, ok := b.open[openKey(pkg, test)]; ok && !occ.terminal {
		return occ
	}
	occ := &Occurrence{}
	t.Occurrences = append(t.Occurrences, occ)
	b.open[openKey(pkg, test)] = occ
	return occ
}

func (b *Builder) appendLine(p *Package, test, line string) {
	if test == "" {
		p.Lines = append(p.Lines, line)
		return
	}
	t, ok := p.Tests[test]
	if !ok {
		t = &Test{Name: test}
		p.Tests[test] = t
		p.TestOrder = append(p.TestOrder, test)
	}
	if len(t.Occurrences) == 0 {
		t.Occurrences = append(t.Occurrences, &Occurrence{})
		b.open[openKey(p.Name, test)] = t.Occurrences[0]
	}
	occ := t.Occurrences[len(t.Occurrences)-1]
	occ.Lines = append(occ.Lines, line)
}

// Finish flushes buffered output and classifies every occurrence that never
// reached a terminal event.
func (b *Builder) Finish() *Run {
	for _, rem := range b.joiner.Flush() {
		if p, ok := b.run.Packages[rem.Package]; ok {
			b.appendLine(p, rem.Test, rem.Text)
		}
	}

	for _, name := range b.run.PackageOrder {
		p := b.run.Packages[name]
		timedOut := packageTimedOut(p)
		for _, tn := range p.TestOrder {
			for _, occ := range p.Tests[tn].Occurrences {
				if occ.terminal {
					continue
				}
				occ.Status = classifyNonTerminal(strings.Join(occ.Lines, "\n"), timedOut)
			}
		}
	}
	return b.run
}

// packageTimedOut reports whether the timeout banner appears ANYWHERE in the
// package -- its own output or any test's.
//
// This is package-wide rather than per-test because of a measured asymmetry.
// When the deadline fires with several tests hung, the runtime prints one
// banner and test2json attributes it to a single test: with TestHangA, B and C
// all sleeping, only TestHangC carried "panic: test timed out after 2s". A and
// B had no panic text at all. Classifying on a test's own output would report
// three identically-hung tests as one timeout and two aborteds.
func packageTimedOut(p *Package) bool {
	const banner = "panic: test timed out"
	for _, line := range p.Lines {
		if strings.Contains(line, banner) {
			return true
		}
	}
	for _, tn := range p.TestOrder {
		for _, occ := range p.Tests[tn].Occurrences {
			for _, line := range occ.Lines {
				if strings.Contains(line, banner) {
					return true
				}
			}
		}
	}
	return false
}

// classifyNonTerminal decides the status of a test that started and never
// finished.
//
// A timeout is reported as `timeout` rather than folded into `error`: the wire
// carries the status and qualflare-cli's parser throws it away. Everything else
// that panicked is `error`; a process that vanished without saying anything at
// all is `aborted`.
func classifyNonTerminal(testText string, packageTimedOut bool) string {
	switch {
	case packageTimedOut:
		return StatusTimeout
	case strings.Contains(testText, "panic:"):
		return StatusError
	default:
		return StatusAborted
	}
}

// HasChildren reports whether any other test in the package is a subtest of
// this one. Callers decide what to do about it; this only answers the question.
func (p *Package) HasChildren(name string) bool {
	prefix := name + "/"
	for _, other := range p.TestOrder {
		if other != name && strings.HasPrefix(other, prefix) {
			return true
		}
	}
	return false
}

// FinalStatus is the status of a test's last occurrence, which is the one that
// decided the outcome.
func (t *Test) FinalStatus() string {
	if len(t.Occurrences) == 0 {
		return StatusAborted
	}
	return t.Occurrences[len(t.Occurrences)-1].Status
}
