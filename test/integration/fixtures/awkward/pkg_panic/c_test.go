package pkg_panic

import "testing"

func TestPanickingSubtest(t *testing.T) {
	t.Run("first_ok", func(t *testing.T) {})
	t.Run("second_panics", func(t *testing.T) {
		panic("deliberate panic in a subtest")
	})
	t.Run("third_never_runs", func(t *testing.T) {})
}
