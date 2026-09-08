package pkg_pass

import "testing"

func TestPasses(t *testing.T) {}

func TestSkippedUpFront(t *testing.T) { t.Skip("deliberate") }

func TestSkippedMidway(t *testing.T) {
	t.Log("did some work first")
	t.Skip("gave up halfway")
}

func ExampleThing() {
	Thing()
	// Output: hello
}
