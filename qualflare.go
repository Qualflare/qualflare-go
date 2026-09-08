// Package qualflare is the author-facing metadata API for the Qualflare Go
// reporter.
//
//	func TestCheckout(t *testing.T) {
//	    qualflare.Label(t, "feature", "checkout")
//	    qualflare.Tag(t, "smoke")
//	    qualflare.Step(t, "add to cart", func() {
//	        qualflare.Parameter(t, "sku", "widget")
//	    })
//	}
//
// Every call takes a testing.TB. Go has no goroutine-local storage, so the
// explicit tb is the only reliable handle on which test is running -- and it
// dissolves a whole class of problem the sibling reporters had to engineer
// around: metadata emitted outside a test is a COMPILE error here, not a
// runtime rule, because there is no tb to pass.
//
// Nothing in this package can fail a test. Every function returns nothing, and
// the one call into testing.TB is guarded. A metadata problem must never be the
// reason somebody's suite goes red.
//
// The results themselves do not come from here. Go has no per-test hook and
// TestMain sees only an exit code, so a library alone can never observe every
// test; the qualflare-go binary reads `go test -json` for that. This package
// only enriches.
package qualflare

import (
	"testing"

	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
	"github.com/Qualflare/qualflare-go/internal/textutil"
)

// Priority levels, matching the wire contract's vocabulary.
const (
	PriorityLow      = "low"
	PriorityMedium   = "medium"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

// Link types. An unrecognised type is passed through and rejected server-side
// rather than silently rewritten here.
const (
	LinkIssue  = "issue"
	LinkTMS    = "tms"
	LinkCustom = "custom"
)

// Label attaches Allure-style name/value metadata (epic, feature, story,
// owner, ...).
func Label(tb testing.TB, name, value string) {
	emit(tb, sentinel.Message{Kind: sentinel.KindLabel, Name: name, Value: value})
}

// Link attaches an external link. linkType is one of LinkIssue, LinkTMS or
// LinkCustom; empty defaults to custom.
func Link(tb testing.TB, url, linkType, name string) {
	if linkType == "" {
		linkType = LinkCustom
	}
	emit(tb, sentinel.Message{Kind: sentinel.KindLink, URL: url, Type: linkType, Name: name})
}

// Tag attaches one or more free-form tags.
func Tag(tb testing.TB, tags ...string) {
	if len(tags) == 0 {
		return
	}
	clipped := make([]string, 0, len(tags))
	for _, t := range tags {
		clipped = append(clipped, textutil.Truncate(t, constants.MaxTagLength))
	}
	emit(tb, sentinel.Message{Kind: sentinel.KindTag, Tags: clipped})
}

// Description attaches prose describing what the test covers.
func Description(tb testing.TB, text string) {
	emit(tb, sentinel.Message{Kind: sentinel.KindDescription, Text: text})
}

// Priority marks the test's importance.
func Priority(tb testing.TB, level string) {
	emit(tb, sentinel.Message{Kind: sentinel.KindPriority, Value: level})
}

// Parameter records a case- or step-level parameter. A parameter emitted while
// a Step is open belongs to that step.
func Parameter(tb testing.TB, name, value string) {
	emit(tb, sentinel.Message{Kind: sentinel.KindParameter, Name: name, Value: value})
}

// MaskedParameter records that a parameter exists without recording its value.
//
// It deliberately takes no value at all. The wire contract treats `masked` as a
// display hint and the server does NOT redact, so withholding the value here is
// the only thing that actually keeps a secret out of the report -- and a
// signature that cannot accept one cannot leak one.
func MaskedParameter(tb testing.TB, name string) {
	emit(tb, sentinel.Message{Kind: sentinel.KindParameter, Name: name, Masked: true})
}
