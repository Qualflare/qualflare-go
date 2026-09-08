package pkg_subtests

import "testing"

// The case qualflare-cli's parser drops entirely: the parent is the only thing
// that failed, so dropping it loses the failure's identity.
func TestParentFailsAllSubtestsPass(t *testing.T) {
	t.Run("sub_one", func(t *testing.T) {})
	t.Run("sub_two", func(t *testing.T) {})
	t.Errorf("parent-level assertion failed after its subtests passed")
}

func TestParentPassesWithSubtests(t *testing.T) {
	t.Run("alpha", func(t *testing.T) {})
	t.Run("beta", func(t *testing.T) {})
}

func TestDeepNesting(t *testing.T) {
	t.Run("a", func(t *testing.T) {
		t.Run("b", func(t *testing.T) {
			t.Run("c", func(t *testing.T) {})
		})
	})
}

func TestSubtestNameWithSpaces(t *testing.T) {
	t.Run("name with spaces", func(t *testing.T) {})
}
