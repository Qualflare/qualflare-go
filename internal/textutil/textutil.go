// Package textutil holds text clamping helpers.
//
// Every limit is measured in RUNES, matching the server's rune counting. Go
// strings are bytes, so a naive slice would both miscount and be able to split a
// multi-byte rune in half; these convert through []rune deliberately.
package textutil

// Truncate clamps s to maxRunes, returning "" for empty input so the caller can
// omit the key entirely.
func Truncate(s string, maxRunes int) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}

// TruncateOr clamps s to maxRunes, falling back to alt when s is empty. It
// exists so a caller can guarantee a non-empty error string without an if.
func TruncateOr(s string, maxRunes int, alt string) string {
	if out := Truncate(s, maxRunes); out != "" {
		return out
	}
	return Truncate(alt, maxRunes)
}
