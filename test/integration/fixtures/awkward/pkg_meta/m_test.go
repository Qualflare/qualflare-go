package pkg_meta

import (
	"testing"

	"github.com/Qualflare/qualflare-go"
)

func TestRecordsEveryKindOfMetadata(t *testing.T) {
	qualflare.Label(t, "team", "platform")
	qualflare.Tag(t, "smoke", "checkout")
	qualflare.Link(t, "https://example.com/issue/42", qualflare.LinkIssue, "QF-42")
	qualflare.Priority(t, qualflare.PriorityHigh)
	qualflare.Description(t, "covers the whole metadata surface")
	qualflare.Parameter(t, "plan", "pro")
	qualflare.MaskedParameter(t, "token")

	qualflare.Step(t, "outer", func() {
		qualflare.Parameter(t, "sku", "widget")
		qualflare.Step(t, "inner", func() {})
	})
}

// A test whose ordinary output contains something sentinel-shaped. It must not
// be consumed as metadata, and must not break decoding of the real messages.
func TestOutputThatLooksLikeASentinel(t *testing.T) {
	t.Log("@@QF1@@ this is not really a sentinel @@deadbeef@@")
	t.Log("@@QF")
	qualflare.Label(t, "after", "the-decoys")
}

func TestMetadataOnASubtestBelongsToTheSubtest(t *testing.T) {
	qualflare.Label(t, "level", "parent")
	t.Run("child", func(t *testing.T) {
		qualflare.Label(t, "level", "child")
	})
}
