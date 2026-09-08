package qualflare

import "sync"

func onceReset() sync.Once { return sync.Once{} }

// stepTB is the fake Step is driven with. It satisfies the narrow `stepper`
// interface rather than testing.TB, which cannot be implemented outside the
// testing package -- that constraint is the reason step() exists separately
// from the exported Step().
type stepTB struct {
	f      *fakeTB
	failed bool
}

func (s stepTB) Failed() bool     { return s.failed }
func (s stepTB) Log(args ...any)  { s.f.Log(args...) }
func (s stepTB) Helper()          { s.f.Helper() }
