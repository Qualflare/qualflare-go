package pkg_timeout_parallel

import (
	"testing"
	"time"
)

// Three tests hung simultaneously when the timeout fires. Only one of them can
// carry the panic banner, so this is the case that decides whether classifying
// a timeout can rely on the test's own output alone.
func TestHangA(t *testing.T) { t.Parallel(); time.Sleep(10 * time.Minute) }
func TestHangB(t *testing.T) { t.Parallel(); time.Sleep(10 * time.Minute) }
func TestHangC(t *testing.T) { t.Parallel(); time.Sleep(10 * time.Minute) }
