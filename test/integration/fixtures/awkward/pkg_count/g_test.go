package pkg_count

import "testing"

// -count=N re-runs the list in ONE process, so a package var counts iterations.
var runs int

func TestFailsOnlyOnTheFirstIteration(t *testing.T) {
	runs++
	if runs == 1 {
		t.Errorf("failing on iteration 1")
	}
}

func TestAlwaysPasses(t *testing.T) {}
