package textutil

import (
	"strings"
	"testing"
)

func TestTruncate_ShortStringsAreUntouched(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestTruncate_EmptyStaysEmpty(t *testing.T) {
	if got := Truncate("", 10); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTruncate_ClampsToTheLimit(t *testing.T) {
	if got := Truncate(strings.Repeat("x", 100), 10); len(got) != 10 {
		t.Errorf("len = %d, want 10", len(got))
	}
}

func TestTruncate_CountsRunesNotBytes(t *testing.T) {
	// The server counts runes. A byte slice would both miscount and be able to
	// split a multi-byte rune in half, producing invalid UTF-8 in the report.
	in := strings.Repeat("é", 20) // 2 bytes each
	got := Truncate(in, 10)
	if n := len([]rune(got)); n != 10 {
		t.Errorf("kept %d runes, want 10", n)
	}
	if !strings.HasSuffix(got, "é") {
		t.Errorf("a rune was split in half: %q", got)
	}
}

func TestTruncate_ExactLengthIsNotClipped(t *testing.T) {
	if got := Truncate("12345", 5); got != "12345" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateOr_FallsBackWhenEmpty(t *testing.T) {
	if got := TruncateOr("", 10, "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
	if got := TruncateOr("real", 10, "fallback"); got != "real" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateOr_ClampsTheFallbackToo(t *testing.T) {
	if got := TruncateOr("", 4, "fallback"); got != "fall" {
		t.Errorf("got %q", got)
	}
}
