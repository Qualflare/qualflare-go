package pkg_exit

import (
	"os"
	"testing"
)

// Exits after passing, so test2json never sees a final PASS/FAIL and emits no
// package-level event at all.
func TestExitsAbruptly(t *testing.T) { os.Exit(3) }
