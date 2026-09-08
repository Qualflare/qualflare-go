package pkg_goroutine_panic

import (
	"testing"
	"time"
)

// A panic on a goroutine the testing framework does not own. Unlike a panic in
// a test body -- which Go recovers and reports as a normal failure -- this kills
// the process outright, so the test never reaches a terminal event.
func TestGoroutinePanic(t *testing.T) {
	go func() { panic("panic from a goroutine the framework does not own") }()
	time.Sleep(5 * time.Second)
}
